package compiler

// Timing analyses over the program's every blocks: a loop-aware estimate of a block's cost, how busy the blocks
// keep the chip between them, which blocks are long enough to make another late, and state machine transitions
// that can never happen because the values they test never change.

import (
	"fmt"
	"sort"
	"strings"
)

const (
	cyclesPerUs = 240        // ESP32 at 240 MHz, as the cycle costs in analyzer.go assume
	loadWarnPct = 70         // warn when the blocks keep the chip busier than this
	maxLoopMult = 1000000000 // cap on nested loop turns, so the estimate can't overflow
)

// wcet walks statements adding up their estimated cycles, multiplying a loop body by its number of turns.
type wcet struct {
	breakdown []costItem
	unbounded []Node // loops whose number of turns isn't known when compiling
}

func (w *wcet) block(stmts []Node, mult int) int {
	total := 0
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s := s.(type) {
		case *ExprStmt:
			cost, name := exprCost(s.Expr)
			total += cost * mult
			if cost > 10 {
				w.breakdown = append(w.breakdown, costItem{name, cost * mult})
			}
		case *VarDecl:
			cost := 1
			if s.Value != nil {
				cost, _ = exprCost(s.Value)
			}
			total += (cost + 1) * mult
		case *AssignExpr:
			cost, _ := exprCost(s.Value)
			total += (cost + 1) * mult
		case *CompoundAssign:
			cost, _ := exprCost(s.Value)
			total += (cost + 2) * mult
		case *ReturnStmt:
			total += 2 * mult
		case *IfStmt:
			// the worse of the two branches
			then := w.block(s.ThenBody, mult)
			els := 0
			switch e := s.ElseBody.(type) {
			case []Node:
				els = w.block(e, mult)
			case *IfStmt:
				els = w.block([]Node{e}, mult)
			}
			total += 5*mult + max(then, els)
		case *ForStmt:
			n, ok := loopTurns(s)
			if !ok {
				w.unbounded = append(w.unbounded, s)
				n = 1
			}
			inner := min(mult*n, maxLoopMult)
			total += 5*inner + w.block(s.Body, inner)
		case *WhileStmt:
			w.unbounded = append(w.unbounded, s)
			total += 5*mult + w.block(s.Body, mult)
		case *EveryBlock:
			total += w.block(s.Body, mult)
		case *OnBlock:
			total += w.block(s.Body, mult)
		case *TimeoutBlock:
			total += w.block(s.TryBody, mult) + w.block(s.FallbackBody, mult)
		case *MatchStmt:
			worst := 0
			for _, arm := range s.Arms {
				if b, ok := arm.Body.([]Node); ok {
					worst = max(worst, w.block(b, mult))
				}
			}
			total += 5*mult + worst
		}
	}
	return total
}

// loopTurns is how many times `for i in a..b step s` runs, when a, b and s are numbers in the code.
func loopTurns(f *ForStmt) (int, bool) {
	a, ok1 := f.Start.(*IntLiteral)
	b, ok2 := f.End.(*IntLiteral)
	if !ok1 || !ok2 {
		return 0, false
	}
	step := 1
	if f.Step != nil {
		s, ok := f.Step.(*IntLiteral)
		if !ok || s.Value == 0 {
			return 0, false
		}
		step = s.Value
	}
	n := (b.Value - a.Value + step - 1) / step
	if step < 0 {
		n = (a.Value - b.Value - step - 1) / -step
	}
	return max(n, 0), true
}

// timedBlock is one every block with a fixed period, and its estimated cost.
type timedBlock struct {
	every  *EveryBlock
	label  string // `every 500ms`, as written
	tier   string
	period int // µs
	cost   int // µs
}

func timedBlocks(program *Program) []timedBlock {
	var out []timedBlock
	for _, t := range program.Tasks {
		for _, s := range walkStmts(t.Body) {
			e, ok := s.(*EveryBlock)
			if !ok {
				continue
			}
			d, ok := e.Interval.(*DurationLiteral)
			if !ok || d.Microseconds <= 0 {
				continue
			}
			var w wcet
			cycles := w.block(e.Body, 1)
			label := "every " + d.Original
			if e.Strict {
				label += " strict"
			}
			out = append(out, timedBlock{every: e, label: label, tier: t.Tier, period: d.Microseconds, cost: cycles / cyclesPerUs})
		}
	}
	return out
}

func fmtUs(us int) string {
	switch {
	case us >= 1000000:
		return fmt.Sprintf("%.1f s", float64(us)/1e6)
	case us >= 1000:
		return fmt.Sprintf("%.1f ms", float64(us)/1e3)
	}
	return fmt.Sprintf("%d µs", us)
}

// checkTiming warns when the every blocks together keep the chip too busy, and when one ordinary block's turn
// is longer than another's period, so that block runs late. Blocks that wait on the network run as their own
// tasks and mostly wait, so they count toward neither.
func checkTiming(program *Program, errs *Errors) {
	blocks := timedBlocks(program)
	var load float64
	var top timedBlock
	for _, b := range blocks {
		if b.tier == "priority" {
			continue
		}
		share := float64(b.cost) / float64(b.period)
		load += share
		if share > float64(top.cost)/float64(max(top.period, 1)) {
			top = b
		}
	}
	if pct := int(load*100 + 0.5); pct > loadWarnPct && top.every != nil {
		errs.Warning(top.every.Region, "TIMING WARNING",
			fmt.Sprintf("The every blocks together keep the chip about %d%% busy, which leaves little time for anything else.", pct),
			fmt.Sprintf("`%s` is the biggest share, about %d%%: run it less often, or make its turn shorter.",
				top.label, int(float64(top.cost)/float64(top.period)*100+0.5)))
	}

	for _, a := range blocks {
		if a.tier != "background" {
			continue
		}
		var late *timedBlock
		for i, b := range blocks {
			if b.every == a.every || b.tier != "background" || a.cost <= b.period {
				continue
			}
			if late == nil || b.period < late.period {
				late = &blocks[i]
			}
		}
		if late != nil {
			errs.Warning(a.every.Region, "TIMING WARNING",
				fmt.Sprintf("A turn of `%s` takes about %s, longer than `%s` waits between turns, so that block runs late while this one works.",
					a.label, fmtUs(a.cost), late.label),
				"Split the work across several turns, or make the other block `strict` if it has to keep exact time.")
		}
	}
}

// ---- transitions that can never happen ----

// checkStuckTransitions warns about a machine transition whose condition is false and stays false, because
// every value it tests keeps its starting value: nothing in the program ever changes it.
func checkStuckTransitions(program *Program, errs *Errors) {
	start := map[string]Node{}
	for _, v := range program.GlobalVars {
		if isLiteral(v.Value) {
			start[v.Name] = v.Value
		}
	}
	for _, m := range program.Machines {
		for _, v := range m.Variables {
			if isLiteral(v.Value) {
				start[v.Name] = v.Value
			}
		}
	}
	if len(start) == 0 {
		return
	}
	for name := range changedVars(program, start) {
		delete(start, name)
	}
	for _, m := range program.Machines {
		for _, s := range m.States {
			for _, tr := range s.Transitions {
				for _, cond := range []Node{tr.Event, tr.Guard} {
					if cond == nil {
						continue
					}
					v, known := constEval(cond, start)
					if b, ok := v.(bool); known && ok && !b {
						names := strings.Join(refsIn(cond, start), "`, `")
						errs.Warning(tr.Region, "STATE MACHINE WARNING",
							fmt.Sprintf("The move to `%s` can never happen: `%s` never changes from its starting value, so the condition is always false.", tr.TargetState, names),
							fmt.Sprintf("Change `%s` somewhere in the program, or remove this transition.", names))
						break
					}
				}
			}
		}
	}
}

func isLiteral(n Node) bool {
	switch n.(type) {
	case *IntLiteral, *BoolLiteral, *FloatLiteral:
		return true
	}
	return false
}

// changedVars lists the variables in start that some statement sets to anything other than their starting value.
func changedVars(program *Program, start map[string]Node) map[string]bool {
	var bodies [][]Node
	for _, t := range program.Tasks {
		bodies = append(bodies, t.Body)
	}
	for _, f := range program.Functions {
		bodies = append(bodies, f.Body)
	}
	if program.Setup != nil {
		bodies = append(bodies, program.Setup.Body)
	}
	if program.Loop != nil {
		bodies = append(bodies, program.Loop.Body)
	}
	for _, m := range program.Machines {
		for _, s := range m.States {
			bodies = append(bodies, s.EnterBody, s.ExitBody, s.Body)
			for _, tr := range s.Transitions {
				bodies = append(bodies, tr.Action)
			}
			for _, tt := range s.TimedTransitions {
				bodies = append(bodies, tt.Action)
			}
		}
	}
	changed := map[string]bool{}
	for _, b := range bodies {
		for _, s := range walkStmts(b) {
			switch s := s.(type) {
			case *CompoundAssign:
				if r, ok := s.Target.(*VarRef); ok {
					changed[r.Name] = true
				}
			case *AssignExpr:
				if r, ok := s.Target.(*VarRef); ok && !sameLiteral(s.Value, start[r.Name]) {
					changed[r.Name] = true
				}
			case *ExprStmt:
				// an assignment can also arrive as an expression statement
				switch e := s.Expr.(type) {
				case *CompoundAssign:
					if r, ok := e.Target.(*VarRef); ok {
						changed[r.Name] = true
					}
				case *AssignExpr:
					if r, ok := e.Target.(*VarRef); ok && !sameLiteral(e.Value, start[r.Name]) {
						changed[r.Name] = true
					}
				}
			}
		}
	}
	return changed
}

func sameLiteral(a, b Node) bool {
	va, ok1 := constEval(a, nil)
	vb, ok2 := constEval(b, nil)
	return ok1 && ok2 && va == vb
}

// constEval works out a condition's value when it only involves numbers, true/false and the given variables.
func constEval(n Node, vars map[string]Node) (any, bool) {
	switch e := n.(type) {
	case *IntLiteral:
		return float64(e.Value), true
	case *FloatLiteral:
		return e.Value, true
	case *BoolLiteral:
		return e.Value, true
	case *VarRef:
		if v, ok := vars[e.Name]; ok {
			return constEval(v, nil)
		}
	case *UnaryExpr:
		v, ok := constEval(e.Operand, vars)
		if !ok {
			return nil, false
		}
		switch x := v.(type) {
		case bool:
			if e.Op == "!" {
				return !x, true
			}
		case float64:
			if e.Op == "-" {
				return -x, true
			}
		}
	case *BinaryExpr:
		l, ok1 := constEval(e.Left, vars)
		r, ok2 := constEval(e.Right, vars)
		if !ok1 || !ok2 {
			return nil, false
		}
		if lb, ok := l.(bool); ok {
			rb, ok := r.(bool)
			if !ok {
				return nil, false
			}
			switch e.Op {
			case "&&":
				return lb && rb, true
			case "||":
				return lb || rb, true
			case "==":
				return lb == rb, true
			case "!=":
				return lb != rb, true
			}
			return nil, false
		}
		lf, ok := l.(float64)
		rf, ok2 := r.(float64)
		if !ok || !ok2 {
			return nil, false
		}
		switch e.Op {
		case "<":
			return lf < rf, true
		case "<=":
			return lf <= rf, true
		case ">":
			return lf > rf, true
		case ">=":
			return lf >= rf, true
		case "==":
			return lf == rf, true
		case "!=":
			return lf != rf, true
		}
	}
	return nil, false
}

// refsIn lists, sorted, the variables from vars that the expression reads.
func refsIn(n Node, vars map[string]Node) []string {
	seen := map[string]bool{}
	for _, e := range walkExprTree(n, nil) {
		if r, ok := e.(*VarRef); ok {
			if _, ok := vars[r.Name]; ok {
				seen[r.Name] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
