package compiler

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Port of transformer.py — Pass 6: AST rewriting for codegen.
//
// Every rewrite copies the nodes it changes (Python's dataclasses.replace)
// and shares the rest, so the analyzed AST is never modified. Like replace,
// a copy drops the node's extra attributes (e.g. `_annotations`,
// `_yield_label`).

// replaceNode is dataclasses.replace(n): a shallow copy without extras.
func replaceNode[T any](n *T) *T {
	c := *n
	v := reflect.ValueOf(&c).Elem()
	for i := range v.NumField() {
		if v.Type().Field(i).Tag.Get("volt") == "extra" {
			v.Field(i).SetZero()
		}
	}
	return &c
}

// yieldCounter numbers yield points (and try blocks) from 1; the
// protothread's case 0 is VOLT_TASK_BEGIN.
type yieldCounter struct{ n int }

func (c *yieldCounter) next() int { c.n++; return c.n }

// transformer holds per-program state.
type transformer struct {
	funcs      map[string]*FuncDecl
	inlineNext int // _inlN_ prefixes, numbered per program
}

// Transform returns the codegen-ready program.
func Transform(program *Program) *Program {
	tr := &transformer{funcs: map[string]*FuncDecl{}}
	for _, f := range program.Functions {
		tr.funcs[f.Name] = f
	}

	autoTasks, setupBody := extractSetupBlocks(program)
	var expanded []*TaskDecl
	for _, t := range append(append([]*TaskDecl{}, program.Tasks...), autoTasks...) {
		expanded = append(expanded, splitMultiEvery(t)...)
	}

	var tasks []*TaskDecl
	var ctxStructs []*StructDecl
	for _, t := range expanded {
		body := tr.inlineYieldingCalls(t.Body)
		// Background tasks re-enter across yields (protothreads), so their
		// bounded-retry counters may need lifting; priority/critical don't.
		body = assignTryIDs(body, &yieldCounter{}, t.Tier == "background")
		body = assignYieldLabels(body, &yieldCounter{})
		t = replaceNode(t)
		t.Body = body
		if t.Tier == "background" && (len(t.CrossingVars) > 0 || anyContextLift(t.Body)) {
			ctxStructs = append(ctxStructs, buildContextStruct(t))
		}
		tasks = append(tasks, t)
	}

	// State machines re-enter across yields too.
	var machines []*MachineDecl
	for _, m := range program.Machines {
		counter := &yieldCounter{}
		var states []*StateDecl
		for _, st := range m.States {
			ns := replaceNode(st)
			if len(st.EnterBody) > 0 {
				ns.EnterBody = assignTryIDs(st.EnterBody, counter, true)
			}
			if len(st.ExitBody) > 0 {
				ns.ExitBody = assignTryIDs(st.ExitBody, counter, true)
			}
			if len(st.Body) > 0 {
				ns.Body = assignTryIDs(st.Body, counter, true)
			}
			states = append(states, ns)
		}
		nm := replaceNode(m)
		nm.States = states
		machines = append(machines, nm)
	}

	for i, t := range tasks {
		t = replaceNode(t)
		t.Body = desugarMatchStmts(t.Body)
		tasks[i] = t
	}
	var funcs []*FuncDecl
	for _, f := range program.Functions {
		f = replaceNode(f)
		f.Body = desugarMatchStmts(f.Body)
		funcs = append(funcs, f)
	}

	out := replaceNode(program)
	out.Tasks, out.Functions, out.Machines = tasks, funcs, machines
	out.Setup, out.Loop = nil, nil
	if program.Setup != nil {
		if setupBody == nil {
			setupBody = program.Setup.Body
		}
		out.Setup = replaceNode(program.Setup)
		out.Setup.Body = desugarMatchStmts(setupBody)
	}
	if program.Loop != nil {
		out.Loop = replaceNode(program.Loop)
		out.Loop.Body = desugarMatchStmts(program.Loop.Body)
	}
	out.Structs = append(append([]*StructDecl{}, program.Structs...), ctxStructs...)
	return out
}

// ---- 0. Setup blocks and multi-every tasks ----

func isConcurrentBlock(s Node) bool {
	switch s := s.(type) {
	case *EveryBlock:
		return !s.Strict
	case *OnBlock:
		return true
	}
	return false
}

// extractSetupBlocks moves every/on blocks out of setup into tasks
// _auto_task_N. It returns the tasks and the remaining setup body, or a
// nil body when nothing was moved.
func extractSetupBlocks(program *Program) ([]*TaskDecl, []Node) {
	if program.Setup == nil {
		return nil, nil
	}
	var tasks []*TaskDecl
	var remaining []Node
	for _, s := range program.Setup.Body {
		if isConcurrentBlock(s) {
			tasks = append(tasks, &TaskDecl{
				Base: Base{regionOf(s)}, Name: fmt.Sprintf("_auto_task_%d", len(tasks)+1),
				Body: []Node{s}, Tier: "background", CrossingVars: []string{},
			})
		} else {
			remaining = append(remaining, s)
		}
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	if remaining == nil {
		remaining = []Node{}
	}
	return tasks, remaining
}

// splitMultiEvery gives each top-level every/on block of a background task
// its own task (name, name_2, ...). Statements before the first block go
// to the first task; other non-block statements are dropped.
func splitMultiEvery(t *TaskDecl) []*TaskDecl {
	if t.Tier != "background" {
		return []*TaskDecl{t}
	}
	n := 0
	for _, s := range t.Body {
		if isConcurrentBlock(s) {
			n++
		}
	}
	if n <= 1 {
		return []*TaskDecl{t}
	}
	var out []*TaskDecl
	var preamble []Node
	for _, s := range t.Body {
		if !isConcurrentBlock(s) {
			preamble = append(preamble, s)
			continue
		}
		sub := replaceNode(t)
		if len(out) == 0 {
			sub.Body = append(append([]Node{}, preamble...), s)
		} else {
			sub.Name = fmt.Sprintf("%s_%d", t.Name, len(out)+1)
			sub.Body = []Node{s}
		}
		sub.CrossingVars = []string{}
		if yields := findYieldPoints(sub.Body); len(yields) > 0 {
			sub.CrossingVars = findCrossingVars(sub.Body, yields)
		}
		out = append(out, sub)
	}
	return out
}

// ---- 1. Context structs ----

// buildContextStruct holds a task's lifted variables and lifted try-retry
// counters.
func buildContextStruct(t *TaskDecl) *StructDecl {
	var fields []*FieldDecl
	for _, v := range topLevelVarDecls(t.Body) {
		if contains(t.CrossingVars, v.Name) {
			fields = append(fields, &FieldDecl{Base: Base{v.Region}, Name: v.Name, TypeName: orStr(v.TypeName, "int")})
		}
	}
	for _, tb := range allTryBlocks(t.Body) {
		if tb.NeedsContextLift {
			fields = append(fields, &FieldDecl{Base: Base{tb.Region}, Name: fmt.Sprintf("_try_attempts_%d", tb.TryID), TypeName: "int"})
		}
	}
	return &StructDecl{Base: Base{t.Region}, Name: t.Name + "_ctx_t", Fields: fields}
}

func anyContextLift(stmts []Node) bool {
	for _, tb := range allTryBlocks(stmts) {
		if tb.NeedsContextLift {
			return true
		}
	}
	return false
}

// allTryBlocks is the transformer's try walk: like walkTryBlocks (blocks
// nested in a try appear twice) but it can't see else-if chains either.
func allTryBlocks(stmts []Node) []*TryBlock {
	var out []*TryBlock
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if tb, ok := s.(*TryBlock); ok {
			out = append(out, tb)
			out = append(out, allTryBlocks(tb.Body)...)
		}
		for _, inner := range listBodies(s) {
			out = append(out, allTryBlocks(inner)...)
		}
	}
	return out
}

// topLevelVarDecls: the VarDecls directly in stmts (not nested).
func topLevelVarDecls(stmts []Node) []*VarDecl {
	var out []*VarDecl
	for _, s := range stmts {
		if v, ok := s.(*VarDecl); ok {
			out = append(out, v)
		}
	}
	return out
}

// ---- Try ids ----

var yieldingMethods = set(
	"wifi.connect", "wifi.disconnect", "wifi.scan", "wifi.on_connect", "wifi.on_disconnect",
	"mqtt.connect", "mqtt.disconnect", "mqtt.on_receive", "mqtt.send", "mqtt.send_int", "mqtt.send_float",
	"http.get", "http.post", "storage.save", "storage.load")

// stmtsContainYield reports whether statements can yield to the scheduler:
// sleep calls, blocking network/storage calls, or nested every/on/timeout.
// A port of Python's generic __dict__ walk: list attributes are walked as
// statements, other node attributes as expressions.
func stmtsContainYield(stmts []Node) bool {
	for _, s := range stmts {
		if yieldWalkStmt(reflect.ValueOf(s)) {
			return true
		}
	}
	return false
}

func isYieldingCall(n Node) bool {
	switch c := n.(type) {
	case *FuncCall:
		return c.Name == "sleep" || c.Name == "deep_sleep" || c.Name == "light_sleep"
	case *MethodCall:
		if name, ok := nameAttr(c.Obj); ok {
			return yieldingMethods[name+"."+c.Method]
		}
	}
	return false
}

// nameAttr is Python's `obj.name` for nodes that have one.
func nameAttr(n Node) (string, bool) {
	if n == nil {
		return "", false
	}
	v := reflect.ValueOf(n).Elem()
	if f, ok := v.Type().FieldByName("Name"); ok && f.Type.Kind() == reflect.String {
		return v.FieldByIndex(f.Index).String(), true
	}
	return "", false
}

// asNode returns v as a non-nil node, unwrapping interfaces.
func asNode(v reflect.Value) (Node, bool) {
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil, false
	}
	n, ok := v.Interface().(Node)
	return n, ok
}

// nodeAttrs lists a node's attribute values (fields and extras, not region).
func nodeAttrs(n Node) []reflect.Value {
	var out []reflect.Value
	v := reflect.ValueOf(n).Elem()
	for i := range v.NumField() {
		if !v.Type().Field(i).Anonymous {
			out = append(out, v.Field(i))
		}
	}
	return out
}

// listValue returns the elements of a slice value (through interfaces).
func listValue(v reflect.Value) ([]reflect.Value, bool) {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]reflect.Value, v.Len())
	for i := range out {
		out[i] = v.Index(i)
	}
	return out, true
}

func yieldWalkStmt(v reflect.Value) bool {
	n, ok := asNode(v)
	if !ok {
		return false
	}
	switch n.(type) {
	case *EveryBlock, *OnBlock, *TimeoutBlock:
		return true
	}
	for _, a := range nodeAttrs(n) {
		if items, ok := listValue(a); ok {
			for _, x := range items {
				xn, isNode := asNode(x)
				_, call := xn.(*FuncCall)
				_, mcall := xn.(*MethodCall)
				if isNode && !call && !mcall {
					if yieldWalkStmt(x) {
						return true
					}
				} else if yieldWalkExpr(x) {
					return true
				}
			}
		} else if _, ok := asNode(a); ok && yieldWalkExpr(a) {
			return true
		}
	}
	return false
}

func yieldWalkExpr(v reflect.Value) bool {
	n, ok := asNode(v)
	if !ok {
		return false
	}
	if isYieldingCall(n) {
		return true
	}
	for _, a := range nodeAttrs(n) {
		if items, ok := listValue(a); ok {
			for _, x := range items {
				if yieldWalkExpr(x) {
					return true
				}
			}
		} else if yieldWalkExpr(a) {
			return true
		}
	}
	return false
}

// assignTryIDs numbers try blocks (outer before inner) and decides whether
// each retry counter must live in the context struct: a bounded retry, in
// code that re-enters across yields, whose body can yield.
func assignTryIDs(stmts []Node, counter *yieldCounter, liftWhenYields bool) []Node {
	out := make([]Node, 0, len(stmts))
	for _, s := range stmts {
		if s == nil {
			out = append(out, s)
			continue
		}
		if tb, ok := s.(*TryBlock); ok {
			id := counter.next()
			body := assignTryIDs(tb.Body, counter, liftWhenYields)
			yields := stmtsContainYield(body)
			n := replaceNode(tb)
			n.Body, n.TryID, n.IsYieldPoint = body, id, yields
			n.NeedsContextLift = tb.Policy == "retry" && tb.RetryLimit != nil && liftWhenYields && yields
			out = append(out, n)
			continue
		}
		rec := func(b []Node) []Node { return assignTryIDs(b, counter, liftWhenYields) }
		switch s := s.(type) {
		case *WhileStmt:
			n := replaceNode(s)
			n.Body = rec(s.Body)
			out = append(out, n)
		case *ForStmt:
			n := replaceNode(s)
			n.Body = rec(s.Body)
			out = append(out, n)
		case *EveryBlock:
			n := replaceNode(s)
			n.Body = rec(s.Body)
			out = append(out, n)
		case *OnBlock:
			n := replaceNode(s)
			n.Body = rec(s.Body)
			out = append(out, n)
		case *TimeoutBlock:
			n := replaceNode(s)
			n.TryBody = rec(s.TryBody)
			n.FallbackBody = rec(s.FallbackBody)
			out = append(out, n)
		case *IfStmt:
			n := replaceNode(s)
			n.ThenBody = rec(s.ThenBody)
			if e, ok := s.ElseBody.([]Node); ok {
				n.ElseBody = rec(e)
			}
			out = append(out, n)
		default:
			out = append(out, s)
		}
	}
	return out
}

// ---- 2. Inlining yielding functions ----

// inlineYieldingCalls replaces `f(args)` statements, where f yields, with
// parameter bindings and f's body under _inlN_-prefixed names.
func (tr *transformer) inlineYieldingCalls(stmts []Node) []Node {
	var out []Node
	for _, s := range stmts {
		if es, ok := s.(*ExprStmt); ok {
			if call, ok := es.Expr.(*FuncCall); ok {
				if f := tr.funcs[call.Name]; f != nil && f.ContainsYield {
					tr.inlineNext++
					prefix := fmt.Sprintf("_inl%d", tr.inlineNext)
					for i, p := range f.Params {
						if i >= len(call.Args) {
							break
						}
						out = append(out, &VarDecl{Base: Base{es.Region}, Name: prefix + "_" + p.Name,
							TypeName: ptr(p.TypeName), Value: call.Args[i], Mutable: true})
					}
					rename := map[string]string{}
					for _, p := range f.Params {
						rename[p.Name] = prefix + "_" + p.Name
					}
					for _, v := range topLevelVarDecls(f.Body) {
						rename[v.Name] = prefix + "_" + v.Name
					}
					out = append(out, tr.inlineYieldingCalls(renameStmts(f.Body, rename))...)
					continue
				}
			}
		}
		switch s := s.(type) {
		case *EveryBlock:
			n := replaceNode(s)
			n.Body = tr.inlineYieldingCalls(s.Body)
			out = append(out, n)
		case *OnBlock:
			n := replaceNode(s)
			n.Body = tr.inlineYieldingCalls(s.Body)
			out = append(out, n)
		case *IfStmt:
			n := replaceNode(s)
			n.ThenBody = tr.inlineYieldingCalls(s.ThenBody)
			n.ElseBody = nil
			switch e := s.ElseBody.(type) {
			case []Node:
				n.ElseBody = tr.inlineYieldingCalls(e)
			case *IfStmt:
				n.ElseBody = tr.inlineYieldingCalls([]Node{e})[0]
			}
			out = append(out, n)
		case *WhileStmt:
			n := replaceNode(s)
			n.Body = tr.inlineYieldingCalls(s.Body)
			out = append(out, n)
		case *ForStmt:
			n := replaceNode(s)
			n.Body = tr.inlineYieldingCalls(s.Body)
			out = append(out, n)
		case *TimeoutBlock:
			n := replaceNode(s)
			n.TryBody = tr.inlineYieldingCalls(s.TryBody)
			n.FallbackBody = tr.inlineYieldingCalls(s.FallbackBody)
			out = append(out, n)
		default:
			out = append(out, s)
		}
	}
	return out
}

func renameStmts(stmts []Node, m map[string]string) []Node {
	out := make([]Node, len(stmts))
	for i, s := range stmts {
		out[i] = renameNode(s, m)
	}
	return out
}

func renamed(name string, m map[string]string) string {
	if r, ok := m[name]; ok {
		return r
	}
	return name
}

// renameNode copies a node with variables renamed. Node types it doesn't
// know (try, timeout, match, literals, ...) are returned as is.
func renameNode(node Node, m map[string]string) Node {
	switch n := node.(type) {
	case nil:
		return nil
	case *VarDecl:
		c := replaceNode(n)
		c.Name, c.Value = renamed(n.Name, m), renameNode(n.Value, m)
		return c
	case *VarRef:
		c := replaceNode(n)
		c.Name = renamed(n.Name, m)
		return c
	case *AssignExpr:
		c := replaceNode(n)
		c.Target, c.Value = renameNode(n.Target, m), renameNode(n.Value, m)
		return c
	case *BinaryExpr:
		c := replaceNode(n)
		c.Left, c.Right = renameNode(n.Left, m), renameNode(n.Right, m)
		return c
	case *UnaryExpr:
		c := replaceNode(n)
		c.Operand = renameNode(n.Operand, m)
		return c
	case *FuncCall:
		c := replaceNode(n)
		c.Args = renameStmts(n.Args, m)
		return c
	case *MethodCall:
		c := replaceNode(n)
		c.Obj, c.Args = renameNode(n.Obj, m), renameStmts(n.Args, m)
		return c
	case *DotAccess:
		c := replaceNode(n)
		c.Obj = renameNode(n.Obj, m)
		return c
	case *ExprStmt:
		c := replaceNode(n)
		c.Expr = renameNode(n.Expr, m)
		return c
	case *ReturnStmt:
		c := replaceNode(n)
		c.Value = renameNode(n.Value, m)
		return c
	case *IfStmt:
		c := replaceNode(n)
		c.Condition, c.ThenBody = renameNode(n.Condition, m), renameStmts(n.ThenBody, m)
		switch e := n.ElseBody.(type) {
		case []Node:
			c.ElseBody = renameStmts(e, m)
		case Node:
			c.ElseBody = renameNode(e, m)
		}
		return c
	case *WhileStmt:
		c := replaceNode(n)
		c.Condition, c.Body = renameNode(n.Condition, m), renameStmts(n.Body, m)
		return c
	case *EveryBlock:
		c := replaceNode(n)
		c.Body = renameStmts(n.Body, m)
		return c
	case *OnBlock:
		c := replaceNode(n)
		c.Body = renameStmts(n.Body, m)
		return c
	case *CompoundAssign:
		c := replaceNode(n)
		c.Target, c.Value = renameNode(n.Target, m), renameNode(n.Value, m)
		return c
	case *OrExpr:
		c := replaceNode(n)
		c.Expr, c.Fallback = renameNode(n.Expr, m), renameNode(n.Fallback, m)
		return c
	case *ForStmt:
		c := replaceNode(n)
		c.VarName = renamed(n.VarName, m)
		c.Start, c.End, c.Step = renameNode(n.Start, m), renameNode(n.End, m), renameNode(n.Step, m)
		c.Body = renameStmts(n.Body, m)
		return c
	}
	return node
}

// ---- 3. Yield labels ----

func assignYieldLabels(stmts []Node, c *yieldCounter) []Node {
	var out []Node
	for _, s := range stmts {
		switch s := s.(type) {
		case *EveryBlock:
			n := replaceNode(s)
			n.YieldLabel = c.next()
			n.Body = assignYieldLabels(s.Body, c)
			out = append(out, n)
		case *OnBlock:
			n := replaceNode(s)
			n.YieldLabel = c.next()
			n.DebounceLabel = 0
			if s.Debounce != nil {
				n.DebounceLabel = c.next()
			}
			n.Body = assignYieldLabels(s.Body, c)
			out = append(out, n)
		case *ExprStmt:
			if fc, ok := s.Expr.(*FuncCall); ok && (fc.Name == "sleep" || fc.Name == "deep_sleep" || fc.Name == "light_sleep") {
				n := replaceNode(s)
				n.YieldLabel = ptr(c.next())
				out = append(out, n)
			} else {
				out = append(out, s)
			}
		case *TimeoutBlock:
			n := replaceNode(s)
			n.TryBody = assignYieldLabels(s.TryBody, c)
			n.FallbackBody = assignYieldLabels(s.FallbackBody, c)
			n.YieldLabel = ptr(c.next()) // after its bodies
			out = append(out, n)
		case *TryBlock:
			n := replaceNode(s)
			n.Body = assignYieldLabels(s.Body, c)
			out = append(out, n)
		case *IfStmt:
			n := replaceNode(s)
			n.ThenBody = assignYieldLabels(s.ThenBody, c)
			n.ElseBody = nil
			switch e := s.ElseBody.(type) {
			case []Node:
				n.ElseBody = assignYieldLabels(e, c)
			case *IfStmt:
				n.ElseBody = assignYieldLabels([]Node{e}, c)[0]
			}
			out = append(out, n)
		case *WhileStmt:
			n := replaceNode(s)
			n.Body = assignYieldLabels(s.Body, c)
			out = append(out, n)
		case *ForStmt:
			n := replaceNode(s)
			n.Body = assignYieldLabels(s.Body, c)
			out = append(out, n)
		default:
			out = append(out, s)
		}
	}
	return out
}

// ---- 4. match → if/else ----

// desugarMatchStmts rewrites match statements into if/else chains. (Blocks
// it copies lose their _yield_label, as in Python.)
func desugarMatchStmts(stmts []Node) []Node {
	var out []Node
	for _, s := range stmts {
		switch s := s.(type) {
		case *MatchStmt:
			if m := desugarMatch(s); m != nil {
				out = append(out, m)
			} else {
				out = append(out, nil)
			}
		case *IfStmt:
			n := replaceNode(s)
			n.ThenBody = desugarMatchStmts(s.ThenBody)
			n.ElseBody = nil
			switch e := s.ElseBody.(type) {
			case []Node:
				n.ElseBody = desugarMatchStmts(e)
			case *IfStmt:
				n.ElseBody = desugarMatchStmts([]Node{e})[0]
			}
			out = append(out, n)
		case *WhileStmt:
			n := replaceNode(s)
			n.Body = desugarMatchStmts(s.Body)
			out = append(out, n)
		case *EveryBlock:
			n := replaceNode(s)
			n.Body = desugarMatchStmts(s.Body)
			out = append(out, n)
		case *OnBlock:
			n := replaceNode(s)
			n.Body = desugarMatchStmts(s.Body)
			out = append(out, n)
		case *ForStmt:
			n := replaceNode(s)
			n.Body = desugarMatchStmts(s.Body)
			out = append(out, n)
		case *TimeoutBlock:
			n := replaceNode(s)
			n.TryBody = desugarMatchStmts(s.TryBody)
			n.FallbackBody = desugarMatchStmts(s.FallbackBody)
			out = append(out, n)
		default:
			out = append(out, s)
		}
	}
	return out
}

// desugarMatch: match (e) { .a -> X, 3 -> Y, _ -> Z } becomes
// if e == a {X} else if e == 3 {Y} else if true {Z}. nil for no arms.
func desugarMatch(m *MatchStmt) *IfStmt {
	var chain *IfStmt
	for i := len(m.Arms) - 1; i >= 0; i-- {
		arm := m.Arms[i]
		var body []Node
		switch b := arm.Body.(type) {
		case []Node:
			body = b
		case *AssignExpr:
			body = []Node{b} // `0 -> x = 1`: an assignment statement, not an expression
		case Node:
			body = []Node{&ExprStmt{Base: Base{arm.Region}, Expr: b}}
		}
		var elseBody any
		if chain != nil {
			elseBody = []Node{chain}
		}
		pattern := strings.TrimSpace(arm.Pattern)
		var cond Node
		lo, hi, isRange := matchRange(pattern)
		if pattern == "_" {
			cond = &BoolLiteral{Base: Base{arm.Region}, Value: true}
		} else if isRange {
			// a..b excludes b, as in for loops.
			cond = &BinaryExpr{Base: Base{arm.Region}, Op: "&&",
				Left:  &BinaryExpr{Base: Base{arm.Region}, Left: m.Expr, Op: ">=", Right: &IntLiteral{Base: Base{arm.Region}, Value: lo}},
				Right: &BinaryExpr{Base: Base{arm.Region}, Left: m.Expr, Op: "<", Right: &IntLiteral{Base: Base{arm.Region}, Value: hi}}}
		} else {
			var right Node
			if n, ok := pyInt(pattern); ok {
				right = &IntLiteral{Base: Base{arm.Region}, Value: n}
			} else {
				right = &VarRef{Base: Base{arm.Region}, Name: strings.TrimLeft(pattern, ".")}
			}
			cond = &BinaryExpr{Base: Base{arm.Region}, Left: m.Expr, Op: "==", Right: right}
		}
		chain = &IfStmt{Base: Base{arm.Region}, Condition: cond, ThenBody: body, ElseBody: elseBody}
	}
	return chain
}

// matchRange parses an `a..b` match pattern.
func matchRange(pattern string) (lo, hi int, ok bool) {
	a, b, found := strings.Cut(pattern, "..")
	if !found {
		return 0, 0, false
	}
	lo, okA := pyInt(strings.TrimSpace(a))
	hi, okB := pyInt(strings.TrimSpace(b))
	return lo, hi, okA && okB
}

// pyInt is Python's int(s) for a stripped string: optional sign, digits,
// single underscores between digits.
func pyInt(s string) (int, bool) {
	t := strings.TrimLeft(s, "+-")
	if len(s)-len(t) > 1 || t == "" || t[0] == '_' || t[len(t)-1] == '_' || strings.Contains(t, "__") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(s, "_", ""))
	return n, err == nil
}
