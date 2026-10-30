package compiler

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Port of checker.py — Pass 3: type checking.
//
// Infers a type for every expression and reports mismatches, wrong argument
// counts, invalid operators and unknown methods. errorType is a poison type
// that absorbs every operation so one mistake is reported once.

const (
	errorType    = "<error>"
	typeInt      = "int"
	typeFloat    = "float"
	typeBool     = "bool"
	typeString   = "string"
	typeVoid     = "void"
	typeDuration = "Duration"
)

// methodSig is a method or builtin function signature.
type methodSig struct {
	Name   string
	Ret    string
	Params []SymParam // TypeName "" accepts any type
}

var (
	methodTypesMu  sync.Mutex
	methodTypes    map[string][]methodSig
	methodTypesReg *DeviceRegistry // the registry methodTypes was built from
)

// allMethodTypes is builtinMethodTypes plus every registry device whose
// name isn't already a builtin type. It is rebuilt whenever the registry
// reloads, so long-running processes (the IDE worker, `volt mcp`) see
// devices added or changed after they started.
func allMethodTypes() map[string][]methodSig {
	reg := Registry()
	methodTypesMu.Lock()
	defer methodTypesMu.Unlock()
	if methodTypes != nil && methodTypesReg == reg {
		return methodTypes
	}
	mt := make(map[string][]methodSig, len(builtinMethodTypes))
	for k, v := range builtinMethodTypes {
		mt[k] = v
	}
	for _, dev := range reg.Devices {
		if _, ok := mt[dev.Name]; ok {
			continue
		}
		ms := []methodSig{}
		for _, m := range dev.Methods {
			ms = append(ms, methodSig{m.Name, m.Returns, m.Params})
		}
		mt[dev.Name] = ms
	}
	methodTypes, methodTypesReg = mt, reg
	return methodTypes
}

func findMethod(ms []methodSig, name string) (methodSig, bool) {
	for _, m := range ms {
		if m.Name == name {
			return m, true
		}
	}
	return methodSig{}, false
}

// methodTypesFor looks up the method table for a value's type: channel<T>
// uses Channel, uart/serial their upper-case tables.
func methodTypesFor(objType string) []methodSig {
	lookup := objType
	switch {
	case strings.HasPrefix(objType, "channel<"):
		lookup = "Channel"
	case objType == "uart" || objType == "serial":
		lookup = strings.ToUpper(objType)
	}
	return allMethodTypes()[lookup]
}

// CheckTypes runs type checking on the whole program.
func CheckTypes(program *Program, scope *Scope, errs *Errors) {
	if program.Hardware != nil {
		inferPinModes(program, scope, errs)
	}

	for _, f := range program.Functions {
		sc := scope.Child("func:" + f.Name)
		for _, p := range f.Params {
			sc.Declare(&Symbol{Name: p.Name, Kind: "param", TypeName: p.TypeName, Region: p.Region})
		}
		checkStmts(f.Body, sc, errs, f.ReturnType)
	}
	for _, t := range program.Tasks {
		checkStmts(t.Body, scope.Child("task:"+t.Name), errs, nil)
	}
	for _, m := range program.Machines {
		sc := scope.Child("machine:" + m.Name)
		for _, st := range m.States {
			checkStmts(st.EnterBody, sc, errs, nil)
			checkStmts(st.ExitBody, sc, errs, nil)
			checkStmts(st.Body, sc, errs, nil)
			for _, tr := range st.Transitions {
				if tr.Guard != nil {
					infer(tr.Guard, sc, errs)
				}
				checkStmts(tr.Action, sc, errs, nil)
			}
			for _, tt := range st.TimedTransitions {
				expectType(tt.Duration, typeDuration, sc, errs, "`after` needs a duration (e.g., 500ms), got `%s`.", unitHint(tt.Duration))
				addUnitFixes(tt.Duration, errs)
				if tt.Guard != nil {
					infer(tt.Guard, sc, errs)
				}
				checkStmts(tt.Action, sc, errs, nil)
			}
		}
	}
	if program.Setup != nil {
		checkStmts(program.Setup.Body, scope.Child("setup"), errs, nil)
	}
	if program.Loop != nil {
		checkStmts(program.Loop.Body, scope.Child("loop"), errs, nil)
	}

	for _, gv := range program.GlobalVars {
		if gv.Value == nil {
			continue
		}
		valType := infer(gv.Value, scope, errs)
		if declared := orStr(gv.TypeName, ""); declared != "" && valType != errorType &&
			valType != normalizeType(declared) {
			errs.Error(gv.Region, "TYPE MISMATCH",
				fmt.Sprintf("Variable `%s` declared as `%s` but assigned `%s`.", gv.Name, declared, valType),
				fmt.Sprintf("Change the type to `%s` or convert the value with `%s(value)`.", valType, declared))
		}
	}
	for _, s := range program.StorageVars {
		if s.DefaultValue == nil {
			continue
		}
		valType := infer(s.DefaultValue, scope, errs)
		if valType != errorType && valType != normalizeType(s.TypeName) {
			errs.Error(s.Region, "TYPE MISMATCH",
				fmt.Sprintf("Storage `%s` declared as `%s` but default value is `%s`.", s.Name, s.TypeName, valType), "")
		}
	}
}

// ---- Pin mode inference ----

var outputMethods = map[string]bool{
	"on": true, "off": true, "toggle": true, "write": true, "is_on": true, "blink": true, "blink_once": true,
}

// inferPinModes gives pins declared without a mode (`pin led = 2`) one from
// the methods called on them: any output method, or none at all, means
// Output; anything else is ambiguous and an error.
func inferPinModes(program *Program, scope *Scope, errs *Errors) {
	var names []string
	pins := map[string]*PinDecl{}
	for _, p := range program.Hardware.Pins {
		if p.Mode != "" {
			continue
		}
		if _, ok := pins[p.Name]; !ok {
			names = append(names, p.Name)
		}
		pins[p.Name] = p
	}
	if len(names) == 0 {
		return
	}

	var all []Node
	for _, t := range program.Tasks {
		all = append(all, t.Body...)
	}
	for _, f := range program.Functions {
		all = append(all, f.Body...)
	}
	if program.Setup != nil {
		all = append(all, program.Setup.Body...)
	}
	if program.Loop != nil {
		all = append(all, program.Loop.Body...)
	}
	// Methods per pin in first-call order. Python keeps a set and names an
	// arbitrary member in the error; the first call is the deterministic choice.
	methods := map[string][]string{}
	for _, n := range names {
		methods[n] = []string{}
	}
	collectPinMethods(all, methods)

	for _, name := range names {
		pin, ms := pins[name], methods[name]
		output := false
		for _, m := range ms {
			output = output || outputMethods[m]
		}
		switch {
		case output || len(ms) == 0:
			pin.Mode = "Output"
		default:
			errs.Error(pin.Region, "PIN MODE",
				fmt.Sprintf("Pin `%s` uses `.%s()` which could be Input, PullUp, Analog, or PullDown. Add an explicit mode.", name, ms[0]),
				fmt.Sprintf("Example: `pin %s: Analog = %d`", name, pin.GPIO))
		}
		if pin.Mode != "" {
			if sym := scope.Lookup(name); sym != nil {
				sym.TypeName = pin.Mode
			}
		}
	}
}

// collectPinMethods records `pin.method()` expression statements, recursing
// into body, then_body, else_body, try_body and fallback_body.
func collectPinMethods(stmts []Node, methods map[string][]string) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		if es, ok := stmt.(*ExprStmt); ok {
			if mc, ok := es.Expr.(*MethodCall); ok {
				if v, ok := mc.Obj.(*VarRef); ok {
					if ms, ok := methods[v.Name]; ok && !contains(ms, mc.Method) {
						methods[v.Name] = append(ms, mc.Method)
					}
				}
			}
		}
		var subs []any
		switch s := stmt.(type) {
		case *IfStmt:
			subs = []any{s.ThenBody, s.ElseBody}
		case *WhileStmt:
			subs = []any{s.Body}
		case *ForStmt:
			subs = []any{s.Body}
		case *EveryBlock:
			subs = []any{s.Body}
		case *OnBlock:
			subs = []any{s.Body}
		case *TimeoutBlock:
			subs = []any{s.TryBody, s.FallbackBody}
		case *TryBlock:
			subs = []any{s.Body}
		}
		for _, sub := range subs {
			switch b := sub.(type) {
			case []Node:
				collectPinMethods(b, methods)
			case Node:
				collectPinMethods([]Node{b}, methods)
			}
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ---- Statements ----

func checkStmts(stmts []Node, scope *Scope, errs *Errors, ret *string) {
	for _, s := range stmts {
		checkStmt(s, scope, errs, ret)
	}
}

// bodyHasCall reports whether a try body contains a function or method call
// (the dead-policy warning: without one, no error_t can be produced).
func bodyHasCall(stmts []Node) bool {
	for _, s := range stmts {
		if stmtHasCall(s) {
			return true
		}
	}
	return false
}

func stmtHasCall(s Node) bool {
	switch s := s.(type) {
	case nil:
		return false
	case *ExprStmt:
		return exprHasCall(reflect.ValueOf(s.Expr))
	case *VarDecl:
		return exprHasCall(reflect.ValueOf(s.Value))
	case *AssignExpr:
		return exprHasCall(reflect.ValueOf(s.Value))
	case *IfStmt:
		if bodyHasCall(s.ThenBody) {
			return true
		}
		switch e := s.ElseBody.(type) {
		case []Node:
			return bodyHasCall(e)
		case *IfStmt:
			// Python iterates the IfStmt itself here and crashes.
			return stmtHasCall(e)
		}
		return false
	case *TimeoutBlock:
		return bodyHasCall(s.TryBody) || bodyHasCall(s.FallbackBody)
	}
	// Other blocks: recurse into a `body` list if there is one.
	if v := reflect.ValueOf(s).Elem().FieldByName("Body"); v.IsValid() {
		if body, ok := v.Interface().([]Node); ok {
			return bodyHasCall(body)
		}
	}
	return false
}

// exprHasCall walks every node-valued field (Python walks __dict__).
func exprHasCall(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return false
		}
		switch v.Interface().(type) {
		case *FuncCall, *MethodCall:
			return true
		}
		return exprHasCall(v.Elem())
	case reflect.Struct:
		if v.Type() == regionType || v.Type() == dictType {
			return false
		}
		for i := 0; i < v.NumField(); i++ {
			if exprHasCall(v.Field(i)) {
				return true
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if exprHasCall(v.Index(i)) {
				return true
			}
		}
	}
	return false
}

func checkStmt(stmt Node, scope *Scope, errs *Errors, ret *string) {
	switch s := stmt.(type) {
	case *VarDecl:
		// `var x = y = 5` is a mistake.
		if _, ok := s.Value.(*AssignExpr); ok {
			errs.Error(s.Region, "SYNTAX ERROR",
				fmt.Sprintf("Invalid initializer for `%s` — did you mean `var %s: type = value`?", s.Name, s.Name),
				"Use `:` for type annotations: `var x: int = 5`")
			return
		}
		valType := errorType
		if s.Value != nil {
			valType = infer(s.Value, scope, errs)
		}
		if declared := orStr(s.TypeName, ""); declared != "" {
			if valType != errorType && valType != normalizeType(declared) {
				errs.Error(s.Region, "TYPE MISMATCH",
					fmt.Sprintf("Variable `%s` declared as `%s` but assigned `%s`.", s.Name, declared, valType),
					fmt.Sprintf("Change the type to `%s` or convert the value with `%s(value)`.", valType, declared))
			}
		} else if valType == errorType && s.Value == nil {
			errs.Error(s.Region, "TYPE ERROR",
				fmt.Sprintf("Variable `%s` has no type annotation and no initial value.", s.Name),
				fmt.Sprintf("Add a type: `var %s: int = 0` or provide a value.", s.Name))
			s.TypeName = ptr("unknown")
		} else {
			s.TypeName = ptr(valType)
		}
		resolved := "unknown"
		if t := orStr(s.TypeName, ""); t != "" {
			resolved = normalizeType(t)
		}
		scope.Declare(&Symbol{Name: s.Name, Kind: "var", TypeName: resolved, Region: s.Region, Mutable: s.Mutable})

	case *AssignExpr:
		switch s.Target.(type) {
		case *VarRef, *DotAccess:
		default:
			errs.Error(s.Region, "SYNTAX ERROR",
				"Cannot assign to this expression — left side must be a variable.",
				"Use `var name = value` to declare, or assign to an existing variable.")
			return
		}
		targetType := infer(s.Target, scope, errs)
		valType := infer(s.Value, scope, errs)
		checkMutable(s.Target, s.Region, scope, errs)
		if targetType != errorType && valType != errorType && targetType != valType {
			errs.Error(s.Region, "TYPE MISMATCH",
				fmt.Sprintf("Cannot assign `%s` to `%s`.", valType, targetType), "")
		}

	case *BreakStmt, *ContinueStmt:
		// Only valid in loops; the analyzer checks that.

	case *CompoundAssign:
		targetType := infer(s.Target, scope, errs)
		valType := infer(s.Value, scope, errs)
		checkMutable(s.Target, s.Region, scope, errs)
		if targetType != errorType && valType != errorType {
			baseOp := s.Op[:1]
			if baseOp == "<" || baseOp == ">" {
				baseOp = s.Op[:2]
			}
			if _, ok := binaryOpTypes[[3]string{targetType, baseOp, valType}]; !ok {
				errs.Error(s.Region, "TYPE MISMATCH",
					fmt.Sprintf("Cannot use `%s` between `%s` and `%s`.", s.Op, targetType, valType), "")
			}
		}

	case *ExprStmt:
		// A field access as a statement is likely a method missing its ().
		if d, ok := s.Expr.(*DotAccess); ok {
			obj := "?"
			if v, ok := d.Obj.(*VarRef); ok {
				obj = v.Name
			}
			errs.Error(s.Region, "SYNTAX ERROR",
				fmt.Sprintf("Did you mean `%s.%s()`?", obj, d.FieldName),
				"Method calls require parentheses: `.method()`")
		} else {
			infer(s.Expr, scope, errs)
		}

	case *ReturnStmt:
		want := orStr(ret, "")
		if s.Value != nil {
			valType := infer(s.Value, scope, errs)
			if want != "" && valType != errorType && valType != want {
				errs.Error(s.Region, "TYPE MISMATCH",
					fmt.Sprintf("Function returns `%s` but got `%s`.", want, valType), "")
			}
		} else if want != "" && want != typeVoid {
			errs.Error(s.Region, "TYPE MISMATCH",
				fmt.Sprintf("Function expects return type `%s` but got empty return.", want), "")
		}

	case *IfStmt:
		expectType(s.Condition, typeBool, scope, errs, "Condition must be `bool`, got `%s`.",
			"Use a comparison like `x > 0` or a boolean function.")
		checkStmts(s.ThenBody, scope, errs, ret)
		switch e := s.ElseBody.(type) {
		case []Node:
			checkStmts(e, scope, errs, ret)
		case *IfStmt:
			checkStmt(e, scope, errs, ret)
		}

	case *WhileStmt:
		expectType(s.Condition, typeBool, scope, errs, "While condition must be `bool`, got `%s`.",
			"Use a comparison like `x > 0` or a boolean function.")
		checkStmts(s.Body, scope, errs, ret)

	case *ForStmt:
		// Both bounds are inferred before either is reported.
		start, end := infer(s.Start, scope, errs), infer(s.End, scope, errs)
		if start != errorType && start != typeInt {
			errs.Error(regionOf(s.Start), "TYPE MISMATCH", fmt.Sprintf("For range start must be `int`, got `%s`.", start), "")
		}
		if end != errorType && end != typeInt {
			errs.Error(regionOf(s.End), "TYPE MISMATCH", fmt.Sprintf("For range end must be `int`, got `%s`.", end), "")
		}
		if s.Step != nil {
			expectType(s.Step, typeInt, scope, errs, "For step must be `int`, got `%s`.", "")
		}
		sc := scope.Child("for:" + s.VarName)
		sc.Declare(&Symbol{Name: s.VarName, Kind: "var", TypeName: typeInt, Region: s.Region})
		checkStmts(s.Body, sc, errs, ret)

	case *EveryBlock:
		expectType(s.Interval, typeDuration, scope, errs, "Interval must be a duration (e.g., 500ms), got `%s`.", unitHint(s.Interval))
		addUnitFixes(s.Interval, errs)
		// A zero interval spins the scheduler without yielding.
		if d, ok := s.Interval.(*DurationLiteral); ok && d.Microseconds <= 0 {
			errs.Error(d.Region, "DURATION ERROR",
				"`every` interval must be greater than zero — a zero-duration every block would spin without yielding.",
				"Use a real interval like `every 1ms` for the fastest meaningful rate.")
		}
		if s.Guard != nil {
			expectType(s.Guard, typeBool, scope, errs, "`when` guard must be `bool`, got `%s`.", "")
		}
		checkStmts(s.Body, scope, errs, ret)

	case *OnBlock:
		infer(s.Event, scope, errs)
		if s.Debounce != nil {
			expectType(s.Debounce, typeDuration, scope, errs, "Debounce must be a duration, got `%s`.", "")
		}
		if s.Throttle != nil {
			expectType(s.Throttle, typeDuration, scope, errs, "Throttle must be a duration, got `%s`.", "")
		}
		checkStmts(s.Body, scope, errs, ret)

	case *TimeoutBlock:
		expectType(s.Duration, typeDuration, scope, errs, "Timeout must be a duration, got `%s`.", "")
		checkStmts(s.TryBody, scope, errs, ret)
		checkStmts(s.FallbackBody, scope, errs, ret)

	case *TryBlock:
		checkStmts(s.Body, scope, errs, ret)
		// Conservative: only the presence of a call is checked, not whether
		// it can actually fail.
		if !bodyHasCall(s.Body) {
			policy := s.Policy
			if s.RetryLimit != nil {
				policy = fmt.Sprintf("retry %d times", *s.RetryLimit)
			}
			errs.Warning(s.Region, "DEAD POLICY",
				fmt.Sprintf("`try` block has no error-returning statements; `or %s` is dead code.", policy),
				"Remove the `try` wrapper, or add a call that can fail (wifi/mqtt/storage/device-init).")
		}

	case *MatchStmt:
		infer(s.Expr, scope, errs)
		for _, arm := range s.Arms {
			switch b := arm.Body.(type) {
			case []Node:
				checkStmts(b, scope, errs, ret)
			case Node:
				infer(b, scope, errs)
			}
		}
	}
}

// expectType infers e and reports msg (with the actual type) at e's region
// unless it is want.
func expectType(e Node, want string, scope *Scope, errs *Errors, msg, hint string) {
	if t := infer(e, scope, errs); t != errorType && t != want {
		errs.Error(regionOf(e), "TYPE MISMATCH", fmt.Sprintf(msg, t), hint)
	}
}

// checkMutable reports assignment to a `let` variable.
func checkMutable(target Node, r Region, scope *Scope, errs *Errors) {
	v, ok := target.(*VarRef)
	if !ok {
		return
	}
	if sym := scope.Lookup(v.Name); sym != nil && !sym.Mutable && sym.Kind == "var" {
		errs.Error(r, "MUTABILITY ERROR",
			fmt.Sprintf("Cannot assign to `%s` — it was declared with `let` (immutable).", v.Name),
			"Use `var` instead of `let` to make it mutable.")
	}
}

// bareNumber is the text of a number literal, or "" for anything else.
func bareNumber(e Node) string {
	switch v := e.(type) {
	case *IntLiteral:
		return strconv.Itoa(v.Value)
	case *FloatLiteral:
		return strconv.FormatFloat(v.Value, 'f', -1, 64)
	}
	return ""
}

// unitHint suggests units for a bare number where a duration belongs.
func unitHint(e Node) string {
	n := bareNumber(e)
	if n == "" {
		return ""
	}
	return fmt.Sprintf("Did you mean `%sms` or `%ss`? Durations always carry a unit.", n, n)
}

// addUnitFixes gives the type error just reported on a bare number one-click units.
func addUnitFixes(e Node, errs *Errors) {
	n := bareNumber(e)
	if n == "" || len(errs.List) == 0 || errs.List[len(errs.List)-1].Region != regionOf(e) {
		return
	}
	r := regionOf(e)
	errs.List[len(errs.List)-1].Fixes = []Fix{{Title: "Make it " + n + "ms", Region: r, Text: n + "ms"}, {Title: "Make it " + n + "s", Region: r, Text: n + "s"}}
}

func ptr[T any](v T) *T { return &v }

// ---- Expressions ----

// normalizeType maps sized integer aliases to int.
func normalizeType(t string) string {
	switch t {
	case "u8", "u16", "u32", "i8", "i16", "i32":
		return typeInt
	}
	return t
}

// infer returns the type of expr, or errorType.
func infer(expr Node, scope *Scope, errs *Errors) string {
	switch e := expr.(type) {
	case *IntLiteral:
		return typeInt
	case *FloatLiteral:
		return typeFloat
	case *BoolLiteral:
		return typeBool
	case *StringLiteral:
		return typeString
	case *DurationLiteral:
		return typeDuration
	case *ArrayLiteral:
		if len(e.Elements) == 0 {
			return "list"
		}
		return "list<" + infer(e.Elements[0], scope, errs) + ">"

	case *VarRef:
		sym := scope.Lookup(e.Name)
		if sym == nil {
			return errorType // reported by the resolver
		}
		return normalizeType(sym.TypeName)

	case *BinaryExpr:
		l, r := infer(e.Left, scope, errs), infer(e.Right, scope, errs)
		if l == errorType || r == errorType {
			return errorType
		}
		result, ok := binaryOpTypes[[3]string{l, e.Op, r}]
		if !ok {
			// == and != between values of the same enum
			if sym := scope.Lookup(l); l == r && (e.Op == "==" || e.Op == "!=") && sym != nil && sym.Kind == "enum" {
				result = typeBool
			} else {
				hint := ""
				switch {
				case l == typeInt && r == typeFloat:
					hint = "Convert with `float(value)` to match types."
				case l == typeFloat && r == typeInt:
					hint = "Convert with `int(value)` to match types."
				case l == typeString && r != typeString:
					hint = "Convert to string with string interpolation: \"{value}\"."
				}
				errs.Error(e.Region, "TYPE MISMATCH",
					fmt.Sprintf("Cannot use `%s` between `%s` and `%s`.", e.Op, l, r), hint)
				return errorType
			}
		}
		e.ResultType = ptr(result)
		return result

	case *UnaryExpr:
		t := infer(e.Operand, scope, errs)
		switch {
		case t == errorType:
			return errorType
		case e.Op == "!" && t != typeBool:
			errs.Error(e.Region, "TYPE MISMATCH",
				fmt.Sprintf("Cannot negate `%s` with `!`. Only `bool` allowed.", t), "")
			return errorType
		case e.Op == "-" && t != typeInt && t != typeFloat:
			errs.Error(e.Region, "TYPE MISMATCH", fmt.Sprintf("Cannot negate `%s` with `-`.", t), "")
			return errorType
		}
		return t

	case *DotAccess:
		// EnumName.variant (the variant itself isn't validated)
		if v, ok := e.Obj.(*VarRef); ok {
			if sym := scope.Lookup(v.Name); sym != nil && sym.Kind == "enum" {
				return sym.TypeName
			}
		}
		t := infer(e.Obj, scope, errs)
		if t == errorType {
			return errorType
		}
		if m, ok := findMethod(methodTypesFor(t), e.FieldName); ok {
			return m.Ret
		}
		return "unknown" // could be a struct field

	case *OrExpr:
		t := infer(e.Expr, scope, errs)
		switch e.Kind {
		case "skip", "restart": // no value produced
		case "retry":
			if e.Fallback != nil {
				infer(e.Fallback, scope, errs) // the retry count
			}
		default:
			fb := infer(e.Fallback, scope, errs)
			intFloat := (t == typeInt && fb == typeFloat) || (t == typeFloat && fb == typeInt)
			if t != errorType && fb != errorType && t != fb && !intFloat {
				errs.Error(e.Region, "TYPE MISMATCH",
					fmt.Sprintf("`or` fallback is `%s` but expression returns `%s`.", fb, t), "")
			}
		}
		return t

	case *FuncCall:
		if b, ok := builtinFuncs[e.Name]; ok {
			if e.Name != "log" && e.Name != "run" { // these take any args
				checkArgCount(e.Name, len(e.Args), len(b.Params), e.Region, errs)
			}
			for _, a := range e.Args {
				infer(a, scope, errs)
			}
			return b.Ret
		}
		sym := scope.Lookup(e.Name)
		if sym == nil || sym.Kind != "func" {
			return errorType // unknown names were reported by the resolver
		}
		checkArgCount(e.Name, len(e.Args), len(sym.Params), e.Region, errs)
		for i, a := range e.Args {
			t := infer(a, scope, errs)
			if i < len(sym.Params) {
				if want := sym.Params[i].TypeName; t != errorType && t != want {
					errs.Error(regionOf(a), "TYPE MISMATCH",
						fmt.Sprintf("Argument %d of `%s` expects `%s`, got `%s`.", i+1, e.Name, want, t), "")
				}
			}
		}
		return orStr(sym.ReturnType, typeVoid)

	case *MethodCall:
		t := infer(e.Obj, scope, errs)
		if t == errorType {
			return errorType
		}
		methods := methodTypesFor(t)
		m, ok := findMethod(methods, e.Method)
		if !ok {
			known := make([]string, len(methods))
			for i, m := range methods {
				known[i] = m.Name
			}
			hint := ""
			if s, ok := suggestSimilar(e.Method, known, 2); ok {
				hint = fmt.Sprintf("Did you mean `%s`?", s)
			}
			if len(known) > 0 {
				hint += " Available: " + strings.Join(known, ", ")
			}
			errs.Error(e.Region, "TYPE MISMATCH",
				fmt.Sprintf("Type `%s` has no method `%s`.", t, e.Method), strings.TrimSpace(hint))
			return errorType
		}
		checkArgCount(e.Method, len(e.Args), len(m.Params), e.Region, errs)
		for i, a := range e.Args {
			at := infer(a, scope, errs)
			if i < len(m.Params) {
				if want := m.Params[i].TypeName; want != "" && at != errorType && at != want {
					errs.Error(regionOf(a), "TYPE MISMATCH",
						fmt.Sprintf("Argument %d of `%s` expects `%s`, got `%s`.", i+1, e.Method, want, at), "")
				}
			}
		}
		return m.Ret
	}
	return errorType
}

func checkArgCount(name string, actual, expected int, r Region, errs *Errors) {
	if actual == expected {
		return
	}
	s := "s"
	if expected == 1 {
		s = ""
	}
	errs.Error(r, "ARGUMENT ERROR", fmt.Sprintf("`%s` expects %d argument%s, got %d.", name, expected, s, actual), "")
}
