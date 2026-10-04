package ide

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PhilemonChiro/volt-compiler"
)

// SequenceResult: the whole program as a Mermaid sequenceDiagram; empty
// when the source doesn't parse.
type SequenceResult struct {
	Mermaid string `json:"mermaid"`
}

// seqMaxMessages caps one diagram; a longer program ends with a note.
const seqMaxMessages = 600

// SequenceDiagram walks every running unit in program order — setup, then
// each timer, task and machine — each as its own participant and labelled
// section: calls to devices, pins, channels, functions and the runtime,
// replies from reads, writes to shared variables. if / match / timeout
// become alt frames, loops and `every` loop frames, `on` opt frames, a
// machine's states sections. Functions show as calls. Frames with nothing
// inside are left out.
func SequenceDiagram(source string) SequenceResult {
	prog, err := compiler.Parse(source)
	if err != nil {
		return SequenceResult{}
	}
	g := SystemGraph(source, nil)
	sb := &seqBuilder{part: map[string]string{},
		funcs: set{}, channels: set{}, pins: set{}, globals: set{}}
	for _, f := range prog.Functions {
		sb.funcs[f.Name] = true
	}
	for _, c := range prog.Channels {
		sb.channels[c.Name] = true
	}
	if prog.Hardware != nil {
		for _, p := range prog.Hardware.Pins {
			sb.pins[p.Name] = true
		}
	}
	for _, v := range prog.GlobalVars {
		sb.globals[v.Name] = true
	}
	for _, s := range prog.StorageVars {
		sb.globals[s.Name] = true
	}
	tasks := map[string]*compiler.TaskDecl{}
	for _, t := range prog.Tasks {
		tasks[t.Name] = t
	}
	machines := map[string]*compiler.MachineDecl{}
	for _, m := range prog.Machines {
		machines[m.Name] = m
	}

	for i, u := range g.Units {
		info, _ := g.Nodes.get(u.ID)
		if info != nil && info.Kind == "func" {
			continue // functions appear as calls
		}
		name := strings.SplitN(u.Name, "<br>", 2)[0]
		if info != nil && info.Kind == "timer" && strings.HasPrefix(name, "↻") {
			name = info.Detail // an unnamed `every` block: "every 1s"
		}
		b := sb
		b.self = b.participant("u"+strconv.Itoa(i), name)
		// A shaded band per unit; its column header already names it.
		b.lines = append(b.lines, "    rect rgba(43, 124, 179, 0.08)")
		n := b.messages
		id := strings.TrimPrefix(u.ID, "u_")
		switch {
		case u.ID == "u_setup" && prog.Setup != nil:
			b.block(prog.Setup.Body)
		case tasks[id] != nil:
			b.block(tasks[id].Body)
		case machines[id] != nil:
			b.machine(machines[id])
		}
		if b.messages == n && machines[id] == nil {
			b.lines = append(b.lines, "    Note over "+b.self+": talks to nothing outside itself")
		}
		b.lines = append(b.lines, "    end")
	}
	if len(sb.order) == 0 {
		return SequenceResult{}
	}
	if sb.capped {
		sb.lines = append(sb.lines, "    Note over "+sb.order[0]+": … (cut short — too long to draw)")
	}
	out := []string{"sequenceDiagram"}
	for _, id := range sb.order {
		out = append(out, "    participant "+id+" as "+sb.part[id])
	}
	out = append(out, sb.lines...)
	return SequenceResult{Mermaid: strings.Join(out, "\n")}
}

type seqBuilder struct {
	self                           string            // participant id of the unit being walked
	part                           map[string]string // participant id → label
	order                          []string
	funcs, channels, pins, globals set
	lines                          []string
	messages                       int
	capped                         bool
}

func (b *seqBuilder) participant(id, label string) string {
	if _, ok := b.part[id]; !ok {
		b.part[id] = seqText(label)
		b.order = append(b.order, id)
	}
	return id
}

func (b *seqBuilder) emit(line string) {
	if b.messages >= seqMaxMessages {
		b.capped = true
		return
	}
	b.messages++
	b.lines = append(b.lines, "    "+line)
}

// frame wraps what body emits in `kind label … end`; dropped if empty.
func (b *seqBuilder) frame(kind, label string, body func()) {
	start := len(b.lines)
	b.lines = append(b.lines, "    "+kind+" "+seqText(label))
	n := b.messages
	body()
	if b.messages == n {
		b.lines = b.lines[:start]
		return
	}
	b.lines = append(b.lines, "    end")
}

func (b *seqBuilder) text(n compiler.Node) string {
	if isNil(n) {
		return ""
	}
	return compiler.ExprText(n)
}

func (b *seqBuilder) block(body []compiler.Node) {
	for _, s := range body {
		b.stmt(s)
	}
}

func (b *seqBuilder) stmt(n compiler.Node) {
	if isNil(n) {
		return
	}
	switch s := n.(type) {
	case *compiler.ExprStmt:
		b.expr(s.Expr, "")
	case *compiler.VarDecl:
		b.expr(s.Value, s.Name)
		if b.globals[s.Name] {
			b.write(s.Name, s.Value)
		}
	case *compiler.AssignExpr:
		b.assign(s.Target, s.Value)
	case *compiler.CompoundAssign:
		b.assign(s.Target, s.Value)
	case *compiler.ReturnStmt:
		b.expr(s.Value, "")
	case *compiler.IfStmt:
		b.ifChain(s)
	case *compiler.WhileStmt:
		b.expr(s.Condition, "")
		b.frame("loop", "while "+b.text(s.Condition), func() { b.block(s.Body) })
	case *compiler.ForStmt:
		b.frame("loop", "for "+s.VarName+" in "+b.text(s.Start)+".."+b.text(s.End), func() { b.block(s.Body) })
	case *compiler.LoopBlock:
		b.frame("loop", "forever", func() { b.block(s.Body) })
	case *compiler.EveryBlock:
		label := "every " + b.text(s.Interval)
		if !isNil(s.Guard) {
			label += " when " + b.text(s.Guard)
		}
		b.frame("loop", label, func() {
			b.expr(s.Guard, "")
			b.block(s.Body)
		})
	case *compiler.OnBlock:
		b.frame("opt", "on "+b.text(s.Event), func() {
			b.expr(s.Event, "")
			b.block(s.Body)
		})
	case *compiler.TimeoutBlock:
		b.altFrames([]seqArm{
			{"within " + b.text(s.Duration), func() { b.block(s.TryBody) }},
			{"timed out", func() { b.block(s.FallbackBody) }},
		})
	case *compiler.TryBlock:
		b.frame("opt", "try ("+s.Policy+")", func() { b.block(s.Body) })
	case *compiler.MatchStmt:
		b.expr(s.Expr, "")
		subject := b.text(s.Expr)
		var arms []seqArm
		for _, a := range s.Arms {
			a := a
			arms = append(arms, seqArm{subject + " is " + a.Pattern, func() {
				if body, ok := a.Body.([]compiler.Node); ok {
					b.block(body)
				} else if nd, ok := a.Body.(compiler.Node); ok {
					b.stmt(nd)
				}
			}})
		}
		b.altFrames(arms)
	case *compiler.BecomeStmt:
		b.emit("Note over " + b.self + ": → " + seqText(s.Target))
	default:
		// Anything else: its calls, in source order.
		children(n, func(v any) { b.anyCalls(v) })
	}
}

func (b *seqBuilder) assign(target, value compiler.Node) {
	b.expr(value, b.text(target))
	if v, ok := target.(*compiler.VarRef); ok && v != nil && b.globals[v.Name] {
		b.write(v.Name, value)
	}
}

func (b *seqBuilder) write(name string, value compiler.Node) {
	b.participant("G", "shared vars")
	val := b.text(value)
	if hasCall(value) {
		val = "(result)" // the call is already on the diagram
	}
	b.emit(b.self + "->>G: " + seqText(clip(name+" = "+val, 60)))
}

// seqPure: builtins that only compute — left off the diagram.
var seqPure = set{"float": true, "int": true, "bool": true, "str": true, "abs": true, "min": true,
	"max": true, "round": true, "floor": true, "ceil": true, "sqrt": true, "pow": true, "len": true}

func hasCall(n any) bool {
	found := false
	var scan func(any)
	scan = func(v any) {
		if found || isNil(v) {
			return
		}
		if items, ok := list(v); ok {
			for _, x := range items {
				scan(x)
			}
			return
		}
		switch c := v.(type) {
		case *compiler.MethodCall:
			found = true
			return
		case *compiler.FuncCall:
			if !seqPure[c.Name] {
				found = true
				return
			}
		}
		children(v, scan)
	}
	scan(n)
	return found
}

type seqArm struct {
	label string
	body  func()
}

// altFrames: `alt a … else b … end`, keeping only arms that emit something
// (an empty first arm still labels the frame when a later one has calls).
func (b *seqBuilder) altFrames(arms []seqArm) {
	start, n := len(b.lines), b.messages
	for i, a := range arms {
		kw := "else"
		if i == 0 {
			kw = "alt"
		}
		b.lines = append(b.lines, "    "+kw+" "+seqText(a.label))
		a.body()
	}
	if b.messages == n {
		b.lines = b.lines[:start]
		return
	}
	b.lines = append(b.lines, "    end")
}

func (b *seqBuilder) ifChain(s *compiler.IfStmt) {
	b.expr(s.Condition, "")
	arms := []seqArm{{b.text(s.Condition), func() { b.block(s.ThenBody) }}}
	for cur := s; ; {
		switch e := cur.ElseBody.(type) {
		case *compiler.IfStmt:
			c := e
			b.expr(c.Condition, "")
			arms = append(arms, seqArm{b.text(c.Condition), func() { b.block(c.ThenBody) }})
			cur = c
			continue
		case []compiler.Node:
			body := e
			arms = append(arms, seqArm{"otherwise", func() { b.block(body) }})
		}
		break
	}
	b.altFrames(arms)
}

// anyCalls walks a subtree for calls (statements nested in odd places).
func (b *seqBuilder) anyCalls(v any) {
	if isNil(v) {
		return
	}
	if items, ok := list(v); ok {
		for _, x := range items {
			b.anyCalls(x)
		}
		return
	}
	if st, ok := v.(compiler.Node); ok {
		switch st.(type) {
		case *compiler.MethodCall, *compiler.FuncCall:
			b.expr(st, "")
			return
		}
	}
	children(v, b.anyCalls)
}

// expr emits the calls in an expression, arguments first. into names what
// a read's reply is stored in (the assignment target), if anything.
func (b *seqBuilder) expr(n compiler.Node, into string) {
	if isNil(n) {
		return
	}
	switch e := n.(type) {
	case *compiler.MethodCall:
		for _, a := range e.Args {
			b.expr(a, "")
		}
		v, ok := e.Obj.(*compiler.VarRef)
		if !ok || v == nil || b.globals[v.Name] {
			b.expr(e.Obj, "")
			return
		}
		var to string
		switch {
		case b.channels[v.Name]:
			to = b.participant("c_"+v.Name, v.Name+" (channel)")
		case b.pins[v.Name]:
			to = b.participant("p_"+v.Name, v.Name+" (pin)")
		default:
			to = b.participant("s_"+v.Name, v.Name)
		}
		b.emit(b.self + "->>" + to + ": " + seqText(e.Method+"("+b.argText(e.Args)+")"))
		if isReadMethod(e.Method) {
			reply := e.Method
			if into != "" {
				reply = into
			}
			b.emit(to + "-->>" + b.self + ": " + seqText(reply))
		}
	case *compiler.FuncCall:
		for _, a := range e.Args {
			b.expr(a, "")
		}
		if b.funcs[e.Name] {
			to := b.participant("f_"+e.Name, e.Name+"()")
			b.emit(b.self + "->>" + to + ": " + seqText(e.Name+"("+b.argText(e.Args)+")"))
			if into != "" {
				b.emit(to + "-->>" + b.self + ": " + seqText(into))
			}
			return
		}
		if seqPure[e.Name] {
			return // conversions and math: not an interaction
		}
		to := b.participant("rt", "runtime")
		b.emit(b.self + "->>" + to + ": " + seqText(e.Name+"("+b.argText(e.Args)+")"))
	default:
		children(n, func(v any) {
			if items, ok := list(v); ok {
				for _, x := range items {
					if nd, ok := x.(compiler.Node); ok {
						b.expr(nd, "")
					}
				}
				return
			}
			if nd, ok := v.(compiler.Node); ok {
				b.expr(nd, "")
			}
		})
	}
}

func (b *seqBuilder) argText(args []compiler.Node) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, b.text(a))
	}
	return clip(strings.Join(parts, ", "), 40)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (b *seqBuilder) machine(m *compiler.MachineDecl) {
	for _, st := range m.States {
		title := "state " + st.Name
		if st.IsInitial {
			title += " (start)"
		}
		b.lines = append(b.lines, "    rect rgba(122, 58, 192, 0.10)")
		b.lines = append(b.lines, "    Note over "+b.self+": "+seqText(title))
		b.frame("opt", "on enter", func() { b.block(st.EnterBody) })
		b.block(st.Body)
		for _, tr := range st.Transitions {
			label := "on " + b.text(tr.Event)
			if !isNil(tr.Guard) {
				label += " if " + b.text(tr.Guard)
			}
			b.frame("opt", label+" → "+tr.TargetState, func() {
				b.expr(tr.Event, "")
				b.block(tr.Action)
			})
		}
		for _, tr := range st.TimedTransitions {
			b.emit("Note over " + b.self + ": " + seqText("after "+b.text(tr.Duration)+" → "+tr.TargetState))
			b.block(tr.Action)
		}
		b.frame("opt", "on exit", func() { b.block(st.ExitBody) })
		b.lines = append(b.lines, "    end")
	}
}

var seqUnsafe = regexp.MustCompile(`[;#\n\r]`)

// seqText makes text safe inside a Mermaid sequence line.
func seqText(s string) string {
	s = seqUnsafe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "<br>", " ")
	if s == "" {
		return "…"
	}
	return s
}
