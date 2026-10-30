package compiler

import (
	"fmt"
	"slices"
)

// Behaviour lints: compile-clean programs that silently don't do what was
// meant. They are warnings, so they never block a build.

// programBodies lists every statement list in the program.
func programBodies(p *Program) [][]Node {
	var out [][]Node
	for _, f := range p.Functions {
		out = append(out, f.Body)
	}
	for _, t := range p.Tasks {
		out = append(out, t.Body)
	}
	if p.Setup != nil {
		out = append(out, p.Setup.Body)
	}
	if p.Loop != nil {
		out = append(out, p.Loop.Body)
	}
	for _, m := range p.Machines {
		for _, st := range m.States {
			out = append(out, st.EnterBody, st.ExitBody, st.Body)
			for _, tr := range st.Transitions {
				out = append(out, tr.Action)
			}
			for _, tr := range st.TimedTransitions {
				out = append(out, tr.Action)
			}
		}
	}
	return out
}

// eachStmt calls fn on every statement in stmts, nested ones included.
func eachStmt(stmts []Node, fn func(Node)) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		fn(s)
		switch s := s.(type) {
		case *IfStmt:
			eachStmt(s.ThenBody, fn)
			switch e := s.ElseBody.(type) {
			case []Node:
				eachStmt(e, fn)
			case *IfStmt:
				eachStmt([]Node{e}, fn)
			}
		case *WhileStmt:
			eachStmt(s.Body, fn)
		case *ForStmt:
			eachStmt(s.Body, fn)
		case *EveryBlock:
			eachStmt(s.Body, fn)
		case *OnBlock:
			eachStmt(s.Body, fn)
		case *TimeoutBlock:
			eachStmt(s.TryBody, fn)
			eachStmt(s.FallbackBody, fn)
		case *TryBlock:
			eachStmt(s.Body, fn)
		case *MatchStmt:
			for _, arm := range s.Arms {
				switch b := arm.Body.(type) {
				case []Node:
					eachStmt(b, fn)
				case Node:
					eachStmt([]Node{b}, fn)
				}
			}
		}
	}
}

// eachExpr calls fn on e and every sub-expression, through `or` too.
func eachExpr(e Node, fn func(Node)) {
	if e == nil {
		return
	}
	fn(e)
	switch e := e.(type) {
	case *BinaryExpr:
		eachExpr(e.Left, fn)
		eachExpr(e.Right, fn)
	case *UnaryExpr:
		eachExpr(e.Operand, fn)
	case *FuncCall:
		for _, a := range e.Args {
			eachExpr(a, fn)
		}
	case *MethodCall:
		eachExpr(e.Obj, fn)
		for _, a := range e.Args {
			eachExpr(a, fn)
		}
	case *AssignExpr:
		eachExpr(e.Target, fn)
		eachExpr(e.Value, fn)
	case *OrExpr:
		eachExpr(e.Expr, fn)
		eachExpr(e.Fallback, fn)
	case *DotAccess:
		eachExpr(e.Obj, fn)
	}
}

// stmtExprs lists the expressions a statement holds directly.
func stmtExprs(s Node) []Node {
	switch s := s.(type) {
	case *VarDecl:
		return []Node{s.Value}
	case *AssignExpr:
		return []Node{s}
	case *CompoundAssign:
		return []Node{s.Target, s.Value}
	case *ExprStmt:
		return []Node{s.Expr}
	case *ReturnStmt:
		return []Node{s.Value}
	case *IfStmt:
		return []Node{s.Condition}
	case *WhileStmt:
		return []Node{s.Condition}
	case *EveryBlock:
		return []Node{s.Guard}
	case *OnBlock:
		return []Node{s.Event}
	}
	return nil
}

// unreachableStates lists the machine states not reachable from the initial
// state, as the analyzer's reachability warning finds them, also following
// `become`.
func unreachableStates(p *Program) map[*StateDecl]bool {
	out := map[*StateDecl]bool{}
	for _, m := range p.Machines {
		if len(m.States) == 0 {
			continue
		}
		byName := map[string]*StateDecl{}
		start := m.States[0]
		for _, st := range m.States {
			byName[st.Name] = st
			if st.IsInitial {
				start = st
			}
		}
		reachable := map[string]bool{}
		queue := []string{start.Name}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			st := byName[cur]
			if reachable[cur] || st == nil {
				continue
			}
			reachable[cur] = true
			for _, tr := range st.Transitions {
				queue = append(queue, tr.TargetState)
			}
			for _, tr := range st.TimedTransitions {
				queue = append(queue, tr.TargetState)
			}
			eachStmt(slices.Concat(st.EnterBody, st.ExitBody, st.Body), func(s Node) {
				if b, ok := s.(*BecomeStmt); ok {
					queue = append(queue, b.Target)
				}
			})
		}
		for _, st := range m.States {
			if !reachable[st.Name] {
				out[st] = true
			}
		}
	}
	return out
}

// warnUnsavedStorage flags storage variables never passed to storage.save:
// they reset to their default on every boot.
func warnUnsavedStorage(p *Program, errs *Errors) {
	saved := map[string]bool{}
	dynamic := false
	for _, body := range programBodies(p) {
		eachStmt(body, func(s Node) {
			for _, e := range stmtExprs(s) {
				eachExpr(e, func(e Node) {
					mc, ok := e.(*MethodCall)
					if !ok || mc.Method != "save" || len(mc.Args) == 0 {
						return
					}
					if v, ok := mc.Obj.(*VarRef); !ok || v.Name != "storage" {
						return
					}
					if key, ok := mc.Args[0].(*StringLiteral); ok {
						saved[key.Value] = true
					} else {
						dynamic = true
					}
				})
			}
		})
	}
	if dynamic {
		return // a computed key might save anything
	}
	for _, sv := range p.StorageVars {
		if !saved[sv.Name] {
			errs.Warning(sv.Region, "UNSAVED STORAGE",
				fmt.Sprintf("Storage variable `%s` is never saved, so it resets to its default on every boot.", sv.Name),
				fmt.Sprintf("Call `storage.save(\"%s\", %s)` after changing it.", sv.Name, sv.Name))
		}
	}
}

// warnUnhandledConnect flags wifi.connect and mqtt.connect called with no `or` fallback and outside a `try`:
// if the network isn't there, the call fails silently and the program carries on offline.
func warnUnhandledConnect(p *Program, errs *Errors) {
	guarded := map[Node]bool{}
	for _, body := range programBodies(p) {
		eachStmt(body, func(s Node) {
			if t, ok := s.(*TryBlock); ok {
				eachStmt(t.Body, func(s Node) { guarded[s] = true })
			}
		})
	}
	for _, body := range programBodies(p) {
		eachStmt(body, func(s Node) {
			es, ok := s.(*ExprStmt)
			if !ok || guarded[s] {
				return
			}
			mc, ok := es.Expr.(*MethodCall)
			if !ok || mc.Method != "connect" {
				return
			}
			if v, ok := mc.Obj.(*VarRef); ok && (v.Name == "wifi" || v.Name == "mqtt") {
				errs.Warning(mc.Region, "UNHANDLED FAILURE",
					fmt.Sprintf("`%s.connect` can fail, and nothing here says what happens then: the program carries on without a connection.", v.Name),
					"Add a fallback: `or retry(5)` to try again, `or skip` to carry on, or `or restart` to reboot.")
			}
		})
	}
}
