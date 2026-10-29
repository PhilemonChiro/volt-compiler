package compiler

import "fmt"

// Port of resolver.py — Pass 2: name resolution.
//
//	Pass 2a: register all top-level declarations (funcs, tasks, structs, pins, ...)
//	Pass 2b: walk all bodies and resolve every name reference.

// builtins exist without being declared in .volt source, in declaration order.
var builtins = [][2]string{
	{"wifi", "WiFi"}, {"mqtt", "MQTT"}, {"http", "HTTP"}, {"storage", "Storage"},
	{"system", "System"}, {"ota", "OTA"}, {"ble", "BLE"},
	{"log", "func"}, {"run", "func"}, {"sleep", "func"},
	{"deep_sleep", "func"}, {"light_sleep", "func"},
	{"true", "bool"}, {"false", "bool"},
	// Type conversion builtins
	{"float", "func"}, {"int", "func"},
	// String builtins
	{"len", "func"}, {"contains", "func"}, {"starts_with", "func"}, {"ends_with", "func"},
	{"to_int", "func"}, {"to_float", "func"},
	// Servo easing constants
	{"linear", "int"}, {"ease_in", "int"}, {"ease_out", "int"}, {"ease_in_out", "int"},
	{"ease_in_cubic", "int"}, {"ease_out_cubic", "int"}, {"ease_in_out_cubic", "int"},
	{"ease_in_sine", "int"}, {"ease_out_sine", "int"}, {"ease_in_out_sine", "int"},
	{"ease_bounce", "int"}, {"ease_back", "int"}, {"ease_elastic", "int"},
}

// Resolve runs two-pass name resolution and returns the global scope.
func Resolve(program *Program, errs *Errors) *Scope {
	global := NewScope(nil, "global")
	for _, b := range builtins {
		global.Declare(&Symbol{Name: b[0], Kind: "builtin", TypeName: b[1]})
	}
	normalizeHardware(program)
	collectDeclarations(program, global, errs)
	resolveBodies(program, global, errs)
	return global
}

// ---- Pass 2a: collect declarations ----

func collectDeclarations(program *Program, scope *Scope, errs *Errors) {
	decl := func(sym *Symbol) { declareOrError(scope, sym, errs) }

	if hw := program.Hardware; hw != nil {
		for _, pin := range hw.Pins {
			decl(&Symbol{Name: pin.Name, Kind: "pin", TypeName: pin.Mode, Region: pin.Region})
		}
		for _, bus := range hw.Buses {
			if bus.Implicit {
				continue
			}
			decl(&Symbol{Name: bus.Name, Kind: "bus", TypeName: bus.BusType, Region: bus.Region})
		}
		for _, s := range hw.Sensors {
			typ := s.SensorType
			if Registry().Get(typ) == nil {
				unknownDevice(s, errs)
				typ = errorType // its method calls add no further errors
			}
			decl(&Symbol{Name: s.Name, Kind: "sensor", TypeName: typ, Region: s.Region})
		}
	}
	for _, s := range program.Structs {
		decl(&Symbol{Name: s.Name, Kind: "struct", TypeName: s.Name, Region: s.Region})
	}
	// Variants are resolved as EnumName.Variant by the checker.
	for _, e := range program.Enums {
		decl(&Symbol{Name: e.Name, Kind: "enum", TypeName: e.Name, Region: e.Region})
	}
	for _, f := range program.Functions {
		params := make([]SymParam, len(f.Params))
		for i, p := range f.Params {
			params[i] = SymParam{p.Name, p.TypeName}
		}
		decl(&Symbol{Name: f.Name, Kind: "func", TypeName: orStr(f.ReturnType, "void"),
			Region: f.Region, Params: params, ReturnType: f.ReturnType})
	}
	for _, t := range program.Tasks {
		decl(&Symbol{Name: t.Name, Kind: "task", TypeName: "task", Region: t.Region})
	}
	for _, m := range program.Machines {
		decl(&Symbol{Name: m.Name, Kind: "machine", TypeName: m.Name, Region: m.Region})
	}
	for _, s := range program.StorageVars {
		decl(&Symbol{Name: s.Name, Kind: "storage", TypeName: s.TypeName, Region: s.Region, Mutable: true})
	}
	for _, ch := range program.Channels {
		decl(&Symbol{Name: ch.Name, Kind: "channel", TypeName: "channel<" + ch.ElemType + ">",
			Region: ch.Region, Mutable: true})
	}
	// Types of untyped globals are inferred from simple literals only;
	// the checker does the rest.
	for _, v := range program.GlobalVars {
		typ := orStr(v.TypeName, "")
		if typ == "" && v.Value != nil {
			switch v.Value.(type) {
			case *IntLiteral:
				typ = "int"
			case *FloatLiteral:
				typ = "float"
			case *BoolLiteral:
				typ = "bool"
			case *StringLiteral:
				typ = "string"
			}
		}
		if typ == "" {
			typ = "unknown"
		}
		decl(&Symbol{Name: v.Name, Kind: "var", TypeName: typ, Region: v.Region, Mutable: v.Mutable})
	}
}

// declareOrError declares sym, reporting a duplicate unless it shadows a
// builtin (in which case the builtin stays).
func declareOrError(scope *Scope, sym *Symbol, errs *Errors) {
	if existing := scope.Declare(sym); existing != nil && existing.Kind != "builtin" {
		errs.Error(sym.Region, "NAME ERROR",
			fmt.Sprintf("Name `%s` is already declared.", sym.Name),
			fmt.Sprintf("First declared at line %d.", existing.Region.StartLine))
	}
}

// orStr is Python's `s or def` for an optional string.
func orStr(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return *s
}

// ---- Pass 2b: resolve names in bodies ----

func resolveBodies(program *Program, global *Scope, errs *Errors) {
	for _, f := range program.Functions {
		sc := global.Child("func:" + f.Name)
		for _, p := range f.Params {
			sc.Declare(&Symbol{Name: p.Name, Kind: "param", TypeName: p.TypeName, Region: p.Region})
		}
		resolveStmts(f.Body, sc, errs)
	}
	for _, t := range program.Tasks {
		resolveStmts(t.Body, global.Child("task:"+t.Name), errs)
	}
	for _, m := range program.Machines {
		sc := global.Child("machine:" + m.Name)
		for _, v := range m.Variables {
			resolveExpr(v.Value, sc, errs)
			sc.Declare(&Symbol{Name: v.Name, Kind: "var", TypeName: orStr(v.TypeName, "unknown"),
				Region: v.Region, Mutable: v.Mutable})
		}
		for _, st := range m.States {
			resolveStmts(st.EnterBody, sc, errs)
			resolveStmts(st.ExitBody, sc, errs)
			resolveStmts(st.Body, sc, errs)
			// Transition events are abstract hardware/system events, not
			// calls, so they aren't resolved. Guards and actions are.
			for _, tr := range st.Transitions {
				resolveExpr(tr.Guard, sc, errs)
				resolveStmts(tr.Action, sc, errs)
			}
			for _, tt := range st.TimedTransitions {
				resolveExpr(tt.Duration, sc, errs)
				resolveExpr(tt.Guard, sc, errs)
				resolveStmts(tt.Action, sc, errs)
			}
		}
	}
	if program.Setup != nil {
		resolveStmts(program.Setup.Body, global.Child("setup"), errs)
	}
	if program.Loop != nil {
		resolveStmts(program.Loop.Body, global.Child("loop"), errs)
	}
}

func resolveStmts(stmts []Node, scope *Scope, errs *Errors) {
	for _, s := range stmts {
		resolveStmt(s, scope, errs)
	}
}

func resolveStmt(stmt Node, scope *Scope, errs *Errors) {
	switch s := stmt.(type) {
	case *VarDecl:
		// The value is resolved before the name is declared.
		resolveExpr(s.Value, scope, errs)
		existing := scope.Declare(&Symbol{Name: s.Name, Kind: "var",
			TypeName: orStr(s.TypeName, "unknown"), Region: s.Region, Mutable: s.Mutable})
		if existing != nil {
			errs.Error(s.Region, "NAME ERROR",
				fmt.Sprintf("Variable `%s` is already declared in this scope.", s.Name),
				fmt.Sprintf("First declared at line %d.", existing.Region.StartLine))
		}
	case *AssignExpr:
		resolveExpr(s.Target, scope, errs)
		resolveExpr(s.Value, scope, errs)
	case *CompoundAssign:
		resolveExpr(s.Target, scope, errs)
		resolveExpr(s.Value, scope, errs)
	case *ExprStmt:
		resolveExpr(s.Expr, scope, errs)
	case *ReturnStmt:
		resolveExpr(s.Value, scope, errs)
	case *IfStmt:
		resolveExpr(s.Condition, scope, errs)
		resolveStmts(s.ThenBody, scope, errs)
		switch e := s.ElseBody.(type) {
		case []Node:
			resolveStmts(e, scope, errs)
		case *IfStmt:
			resolveStmt(e, scope, errs)
		}
	case *WhileStmt:
		resolveExpr(s.Condition, scope, errs)
		resolveStmts(s.Body, scope, errs)
	case *ForStmt:
		resolveExpr(s.Start, scope, errs)
		resolveExpr(s.End, scope, errs)
		resolveExpr(s.Step, scope, errs)
		sc := scope.Child("for:" + s.VarName)
		sc.Declare(&Symbol{Name: s.VarName, Kind: "var", TypeName: "int", Region: s.Region})
		resolveStmts(s.Body, sc, errs)
	case *EveryBlock:
		resolveExpr(s.Interval, scope, errs)
		resolveExpr(s.Guard, scope, errs)
		resolveStmts(s.Body, scope, errs)
	case *OnBlock:
		resolveExpr(s.Event, scope, errs)
		resolveExpr(s.Debounce, scope, errs)
		resolveExpr(s.Throttle, scope, errs)
		resolveStmts(s.Body, scope, errs)
	case *TimeoutBlock:
		resolveExpr(s.Duration, scope, errs)
		resolveStmts(s.TryBody, scope, errs)
		resolveStmts(s.FallbackBody, scope, errs)
	case *TryBlock:
		// Declarations inside the body are scoped to the block.
		resolveStmts(s.Body, NewScope(scope, "try"), errs)
	case *MatchStmt:
		resolveExpr(s.Expr, scope, errs)
		for _, arm := range s.Arms {
			switch b := arm.Body.(type) {
			case []Node:
				resolveStmts(b, scope, errs)
			case Node:
				resolveExpr(b, scope, errs)
			}
		}
	}
}

func resolveExpr(expr Node, scope *Scope, errs *Errors) {
	switch e := expr.(type) {
	case *VarRef:
		if scope.Lookup(e.Name) == nil {
			errs.Error(e.Region, "NAME ERROR", fmt.Sprintf("Unknown name `%s`.", e.Name),
				didYouMean(e.Name, scope))
		}
	case *BinaryExpr:
		resolveExpr(e.Left, scope, errs)
		resolveExpr(e.Right, scope, errs)
	case *UnaryExpr:
		resolveExpr(e.Operand, scope, errs)
	case *DotAccess:
		// The field is a property, not a scope name.
		resolveExpr(e.Obj, scope, errs)
	case *FuncCall:
		if scope.Lookup(e.Name) == nil {
			errs.Error(e.Region, "NAME ERROR", fmt.Sprintf("Unknown function `%s`.", e.Name),
				didYouMean(e.Name, scope))
		}
		for _, a := range e.Args {
			resolveExpr(a, scope, errs)
		}
	case *MethodCall:
		// The method name is validated by the type checker.
		resolveExpr(e.Obj, scope, errs)
		for _, a := range e.Args {
			resolveExpr(a, scope, errs)
		}
	case *ArrayLiteral:
		for _, el := range e.Elements {
			resolveExpr(el, scope, errs)
		}
	case *OrExpr:
		resolveExpr(e.Expr, scope, errs)
		resolveExpr(e.Fallback, scope, errs)
	case *AssignExpr:
		resolveExpr(e.Target, scope, errs)
		resolveExpr(e.Value, scope, errs)
	}
}

func didYouMean(name string, scope *Scope) string {
	if s, ok := suggestSimilar(name, scope.AllNames(), 2); ok {
		return fmt.Sprintf("Did you mean `%s`?", s)
	}
	return ""
}

// suggestSimilar finds the closest known name within maxDistance edits;
// the first candidate wins ties.
func suggestSimilar(name string, known []string, maxDistance int) (string, bool) {
	n := []rune(name)
	best, bestDist := "", maxDistance+1
	for _, cand := range known {
		c := []rune(cand)
		if abs(len(c)-len(n)) > maxDistance {
			continue
		}
		if d := levenshtein(n, c); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best, bestDist <= maxDistance
}

func levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		curr := make([]int, 1, len(b)+1)
		curr[0] = i + 1
		for j, cb := range b {
			cost := 1
			if ca == cb {
				cost = 0
			}
			curr = append(curr, min(curr[j]+1, prev[j+1]+1, prev[j]+cost))
		}
		prev = curr
	}
	return prev[len(b)]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
