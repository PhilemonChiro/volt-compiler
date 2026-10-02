package compiler

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Pass 1: parse Volt source into an AST. A hand-written recursive-descent
// port of grammar.lark + parser.py (Lark LALR with the ASTBuilder
// transformer). It accepts exactly what the Lark grammar accepts and
// builds the same AST, quirks included; the comments on each function give
// the grammar rule it implements.
//
// Regions follow parser.py: most nodes take the region of their first
// kept token or child node (a BinaryExpr's region is its left operand's),
// and nodes built without one (ReturnStmt, ExprStmt, BoolLiteral, ...) keep
// the zero Region.

// SyntaxError is returned by Parse. Line and Col locate the offending token.
type SyntaxError struct {
	Line, Col int
	EndCol    int // column just past the offending token
	Msg       string
	Hint      string // what the parser wanted there, when it knows
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d.", e.Msg, e.Line, e.Col)
}

type parser struct {
	lx  *lexer
	off int // byte offset of the next unconsumed input
}

// Parse parses a Volt source string into a Program.
func Parse(src string) (prog *Program, err error) {
	p := &parser{lx: newLexer(src)}
	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(*SyntaxError)
			if !ok {
				panic(r)
			}
			prog, err = nil, se
		}
	}()
	return p.program(), nil
}

// --- Token plumbing ---

func (p *parser) peek(ctx lexCtx) token { return p.lx.lex(p.off, ctx) }

func (p *parser) take(t token) token {
	p.off = t.end
	return t
}

func (p *parser) next(ctx lexCtx) token { return p.take(p.peek(ctx)) }

func (p *parser) fail(t token, format string, args ...any) {
	p.failHint(t, "", format, args...)
}

func (p *parser) failHint(t token, hint, format string, args ...any) {
	r := p.lx.region(t)
	end := r.EndCol
	if t.kind == tNL || t.kind == tEOF || end <= r.StartCol {
		end = r.StartCol + 1
	}
	panic(&SyntaxError{Line: r.StartLine, Col: r.StartCol, EndCol: end, Msg: fmt.Sprintf(format, args...), Hint: hint})
}

func (p *parser) unexpected(t token) { p.unexpectedWant(t, "") }

// unexpectedWant reports t, naming what was expected (e.g. "`}`") in the hint.
func (p *parser) unexpectedWant(t token, want string) {
	hint := ""
	if want != "" {
		hint = "Expected " + want + " here."
	}
	if t.kind == tEOF {
		p.failHint(t, hint, "Unexpected end of input")
	}
	if t.kind == tNL {
		p.failHint(t, hint, "Unexpected end of line")
	}
	// A word the context doesn't accept lexes as one bad character; report
	// the whole word.
	if w := reName.FindString(p.lx.src[t.start:]); t.kind == tBad && w != "" {
		t.text, t.end = w, t.start+len(w)
	}
	p.failHint(t, hint, "Unexpected token %q", t.text)
}

// failNear reports a non-token error (Python raises inside the
// transformer) at the current position.
func (p *parser) failNear(format string, args ...any) {
	p.fail(p.peek(ctxName), format, args...)
}

func (p *parser) expectSym(s string) token {
	return p.expectSymIn(ctxName, s)
}

func (p *parser) expectSymIn(ctx lexCtx, s string) token {
	t := p.next(ctx)
	if !t.sym(s) {
		p.unexpectedWant(t, "`"+s+"`")
	}
	return t
}

// expectKw consumes keyword kw where NAME is not accepted.
func (p *parser) expectKw(kw string) token {
	t := p.next(prefixCtx(kw))
	if !t.word(kw) {
		p.unexpectedWant(t, "`"+kw+"`")
	}
	return t
}

// name consumes a NAME (any word, keywords included).
func (p *parser) name() token {
	t := p.next(ctxName)
	if t.kind != tName {
		p.unexpectedWant(t, "a name")
	}
	return t
}

func (p *parser) expectNL() {
	if t := p.next(ctxName); t.kind != tNL {
		p.unexpectedWant(t, "a new line")
	}
}

func (p *parser) region(t token) Region { return p.lx.region(t) }

func regionOf(n Node) Region {
	if n == nil {
		return Region{}
	}
	return n.base().Region
}

// --- Program ---

var topKeywords = prefixCtx("chip", "include", "hardware", "power", "struct", "enum", "func", "task",
	"machine", "setup", "loop", "raw", "storage", "shared", "channel", "every", "on")

var ctxTop = lexCtx{kws: topKeywords.kws, varKw: true}

// start: (_NL | top_level)*
func (p *parser) program() *Program {
	prog := &Program{}
	for {
		t := p.peek(ctxTop)
		switch {
		case t.kind == tEOF:
			return prog
		case t.kind == tNL:
			p.take(t)
		case t.kind == tAnnotation || t.word("task"):
			prog.Tasks = append(prog.Tasks, p.taskDecl())
		case t.kind == tVarKw || t.word("shared"):
			prog.GlobalVars = append(prog.GlobalVars, p.varDecl(ctxTop))
			p.expectNL()
		case t.word("chip"):
			p.take(t)
			name := p.name()
			p.expectNL()
			if prog.Chip != nil {
				p.failNear("Multiple chip declarations — already declared '%s' (line %d)",
					prog.Chip.Target, prog.Chip.Region.StartLine)
			}
			prog.Chip = &ChipDecl{Base{p.region(name)}, name.text}
		case t.word("include"):
			// include_decl: "include" ESCAPED_STRING _NL
			p.take(t)
			s := p.next(ctxName)
			if s.kind != tString {
				p.unexpectedWant(s, "a quoted file name")
			}
			p.expectNL()
			if path := strings.Trim(s.text, `"`); strings.HasSuffix(path, ".volt") {
				prog.Includes = append(prog.Includes, path)
			}
		case t.word("hardware"):
			prog.Hardware = p.hardwareBlock()
		case t.word("power"):
			prog.Power = p.powerBlock()
		case t.word("struct"):
			prog.Structs = append(prog.Structs, p.structDecl())
		case t.word("enum"):
			prog.Enums = append(prog.Enums, p.enumDecl())
		case t.word("func"):
			prog.Functions = append(prog.Functions, p.funcDecl())
		case t.word("machine"):
			prog.Machines = append(prog.Machines, p.machineDecl())
		case t.word("setup"):
			p.take(t)
			prog.Setup = &SetupBlock{Body: p.block()}
		case t.word("loop"):
			p.take(t)
			prog.Loop = &LoopBlock{Body: p.block()}
		case t.word("raw"):
			// raw_block: "raw" "{" ESCAPED_STRING "}"
			p.take(t)
			p.expectSym("{")
			s := p.next(ctxName)
			if s.kind != tString {
				p.unexpectedWant(s, "a quoted string")
			}
			p.expectSym("}")
			prog.RawBlocks = append(prog.RawBlocks, &RawBlock{CCode: strings.Trim(s.text, `"`)})
		case t.word("storage"):
			prog.StorageVars = append(prog.StorageVars, p.storageDecl())
			p.expectNL()
		case t.word("channel"):
			prog.Channels = append(prog.Channels, p.channelDecl())
			p.expectNL()
		case t.word("every"), t.word("on"):
			// Top-level every/on blocks become auto-named tasks.
			var rule Node
			p.take(t)
			if t.word("every") {
				rule = p.everyBlock()
			} else {
				rule = p.onBlock(p.expr())
			}
			n := 0
			for _, task := range prog.Tasks {
				if strings.HasPrefix(task.Name, "_rule") {
					n++
				}
			}
			prog.Tasks = append(prog.Tasks, &TaskDecl{
				Base: Base{regionOf(rule)}, Name: fmt.Sprintf("_rule%d", n), Body: []Node{rule},
			})
		default:
			p.unexpectedWant(t, "a top-level declaration (chip, hardware, task, fn, every, …)")
		}
	}
}

// --- Hardware ---

// hardware_block: "hardware" "{" (_NL | hardware_item)* _hw_inline? "}"
// hardware_item: (pin_decl | bus_decl | sensor_decl) _NL
func (p *parser) hardwareBlock() *HardwareBlock {
	p.take(p.peek(ctxTop))
	p.expectSym("{")
	hw := &HardwareBlock{}
	var items []Node
	for done := false; !done; {
		t := p.peek(prefixCtx("pin", "bus", "device"))
		switch {
		case t.kind == tNL:
			p.take(t)
			continue
		case t.sym("}"):
			p.take(t)
			done = true
			continue
		case t.word("pin"):
			d := p.pinDecl()
			hw.Pins = append(hw.Pins, d)
			items = append(items, d)
		case t.word("bus"):
			d := p.busDecl()
			hw.Buses = append(hw.Buses, d)
			items = append(items, d)
		case t.word("device"):
			d := p.sensorDecl()
			hw.Sensors = append(hw.Sensors, d)
			items = append(items, d)
		default:
			p.unexpectedWant(t, "`pin`, `bus`, `device` or `}`")
		}
		switch t := p.next(ctxName); {
		case t.kind == tNL:
		case t.sym("}"): // _hw_inline
			done = true
		default:
			p.unexpectedWant(t, "a new line or `}`")
		}
	}
	if len(items) > 0 {
		first, last := regionOf(items[0]), regionOf(items[len(items)-1])
		hw.Region = Region{first.StartLine, first.StartCol, last.EndLine, last.EndCol}
	}
	return hw
}

// pin_decl: "pin" NAME ":" pin_mode "=" expr
//
//	| "pin" NAME "=" expr -> pin_decl_inferred
//
// pin_mode: NAME ("." NAME)*
func (p *parser) pinDecl() *PinDecl {
	p.take(p.peek(prefixCtx("pin")))
	name := p.name()
	d := &PinDecl{Base: Base{p.region(name)}, Name: name.text}
	t := p.next(ctxName)
	inferred := t.sym("=")
	if !inferred {
		if !t.sym(":") {
			p.unexpectedWant(t, "`:` or `=`")
		}
		parts := []string{p.name().text}
		for p.peek(ctxName).sym(".") {
			p.take(p.peek(ctxName))
			parts = append(parts, p.name().text)
		}
		d.Mode = strings.Join(parts, ".")
		p.expectSym("=")
	}
	// GPIO number from an int, a GP-prefixed name (GP25) or, with an
	// explicit mode only, an STM32-style name (PA5 -> port*16 + pin).
	switch e := p.expr().(type) {
	case *IntLiteral:
		d.GPIO = e.Value
	case *VarRef:
		s := strings.ToUpper(e.Name)
		if strings.HasPrefix(s, "GP") && isDigits(s[2:]) {
			d.GPIO, _ = strconv.Atoi(s[2:])
		} else if !inferred && s[0] == 'P' && len(s) >= 3 && isAlpha(s[1]) && isDigits(s[2:]) {
			n, _ := strconv.Atoi(s[2:])
			d.GPIO = int(s[1]-'A')*16 + n
		}
	}
	return d
}

// isDigits is Python's str.isdigit on ASCII: non-empty, all digits.
func isDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// bus_decl: "bus" NAME ":" NAME "(" named_args? ")"
func (p *parser) busDecl() *BusDecl {
	p.take(p.peek(prefixCtx("bus")))
	name := p.name()
	p.expectSym(":")
	typ := p.name()
	p.expectSym("(")
	return &BusDecl{
		Base: Base{p.region(name)}, BusType: strings.ToLower(typ.text), Name: name.text,
		Params: p.namedArgs(),
	}
}

// sensor_decl: "device" NAME ":" NAME "on" NAME "at" expr
//
//	| "device" NAME ":" NAME "(" named_args? ")" -> sensor_decl_named
func (p *parser) sensorDecl() *SensorDecl {
	p.take(p.peek(prefixCtx("device")))
	name := p.name()
	p.expectSym(":")
	d := &SensorDecl{Base: Base{p.region(name)}, Name: name.text, SensorType: p.name().text}
	switch t := p.next(prefixCtx("on")); {
	case t.word("on"):
		d.BusName = p.name().text
		p.expectKw("at")
		if lit, ok := p.expr().(*IntLiteral); ok {
			d.Address = lit.Value
		}
	case t.sym("("):
		args := p.namedArgs()
		for _, k := range args.Keys {
			v := args.Vals[k]
			if lit, ok := v.(*IntLiteral); ok {
				v = lit.Value
			}
			d.PinParams.Set(k, v)
		}
	default:
		p.unexpectedWant(t, "`on` or `(`")
	}
	return d
}

// named_args? ")" after the "(" — named_args: named_arg ("," named_arg)*
// named_arg: NAME ":" expr
func (p *parser) namedArgs() Dict {
	var d Dict
	if t := p.peek(ctxName); t.sym(")") {
		p.take(t)
		return d
	}
	for {
		k := p.name()
		p.expectSym(":")
		d.Set(k.text, p.expr())
		t := p.next(ctxName)
		if t.sym(")") {
			return d
		}
		if !t.sym(",") {
			p.unexpectedWant(t, "`,` or `)`")
		}
	}
}

// --- Power ---

// power_block: "power" "{" (_NL | power_item)* "}"
// power_item: NAME ":" power_value _NL
// power_value: NAME "(" arg_list ")" | NAME
func (p *parser) powerBlock() *PowerBlock {
	p.take(p.peek(ctxTop))
	p.expectSym("{")
	pb := &PowerBlock{}
	for {
		t := p.next(ctxName)
		switch {
		case t.kind == tNL:
			continue
		case t.sym("}"):
			return pb
		case t.kind != tName:
			p.unexpectedWant(t, "a power setting or `}`")
		}
		p.expectSym(":")
		value := p.name()
		if p.peek(ctxName).sym("(") {
			p.take(p.peek(ctxName))
			p.argList(false)
		}
		p.expectNL()
		pb.Items.Set(t.text, value.text)
	}
}

// --- Struct / enum ---

// struct_decl: "struct" NAME "{" (_NL | struct_field)* "}"
// struct_field: NAME ":" type_name _NL
func (p *parser) structDecl() *StructDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	s := &StructDecl{Base: Base{p.region(name)}, Name: name.text}
	p.expectSym("{")
	for {
		t := p.next(ctxName)
		switch {
		case t.kind == tNL:
			continue
		case t.sym("}"):
			return s
		case t.kind != tName:
			p.unexpectedWant(t, "a field name or `}`")
		}
		p.expectSym(":")
		s.Fields = append(s.Fields, &FieldDecl{Base{p.region(t)}, t.text, p.typeName()})
		p.expectNL()
	}
}

// enum_decl: "enum" NAME "{" enum_body "}"
// enum_body: (_NL | enum_variant)*
// enum_variant: NAME ","? _NL?
func (p *parser) enumDecl() *EnumDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	e := &EnumDecl{Base: Base{p.region(name)}, Name: name.text}
	p.expectSym("{")
	for {
		t := p.next(ctxName)
		switch {
		case t.kind == tNL:
		case t.sym("}"):
			return e
		case t.kind == tName:
			e.Variants = append(e.Variants, t.text)
			if c := p.peek(ctxName); c.sym(",") {
				p.take(c)
			}
		default:
			p.unexpectedWant(t, "an enum variant or `}`")
		}
	}
}

// --- Functions and tasks ---

// func_decl: "func" NAME "(" param_list? ")" return_type? block
// param: NAME ":" type_name;  return_type: "->" type_name
func (p *parser) funcDecl() *FuncDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	f := &FuncDecl{Base: Base{p.region(name)}, Name: name.text}
	p.expectSym("(")
	if t := p.peek(ctxName); t.sym(")") {
		p.take(t)
	} else {
		for {
			pn := p.name()
			p.expectSym(":")
			f.Params = append(f.Params, &Param{Base{p.region(pn)}, pn.text, p.typeName()})
			t := p.next(ctxName)
			if t.sym(")") {
				break
			}
			if !t.sym(",") {
				p.unexpectedWant(t, "`,` or `)`")
			}
		}
	}
	if t := p.peek(ctxName); t.sym("->") {
		p.take(t)
		rt := p.typeName()
		f.ReturnType = &rt
	}
	f.Body = p.block()
	return f
}

// task_decl: TASK_ANNOTATION* "task" NAME block
func (p *parser) taskDecl() *TaskDecl {
	annotations := []string{}
	for {
		t := p.next(prefixCtx("task"))
		if t.kind == tAnnotation {
			annotations = append(annotations, t.text)
			continue
		}
		if !t.word("task") {
			p.unexpectedWant(t, "`task`")
		}
		break
	}
	name := p.name()
	return &TaskDecl{Base: Base{p.region(name)}, Name: name.text, Body: p.block(), Annotations: annotations}
}

// --- State machines ---

// machine_decl: "machine" NAME "{" (_NL | state_decl | var_decl _NL)* "}"
func (p *parser) machineDecl() *MachineDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	m := &MachineDecl{Base: Base{p.region(name)}, Name: name.text}
	p.expectSym("{")
	ctx := lexCtx{kws: prefixCtx("initial", "state", "shared").kws, varKw: true}
	for {
		t := p.peek(ctx)
		switch {
		case t.kind == tNL:
			p.take(t)
			continue
		case t.sym("}"):
			p.take(t)
			// With no state marked initial, the first one is.
			initial := false
			for _, s := range m.States {
				initial = initial || s.IsInitial
			}
			if !initial && len(m.States) > 0 {
				m.States[0].IsInitial = true
			}
			return m
		case t.word("initial"):
			p.take(t)
			p.expectKw("state")
			m.States = append(m.States, p.stateDecl(true))
		case t.word("state"):
			p.take(t)
			m.States = append(m.States, p.stateDecl(false))
		case t.kind == tVarKw || t.word("shared"):
			m.Variables = append(m.Variables, p.varDecl(ctx))
			p.expectNL()
		default:
			p.unexpectedWant(t, "`initial`, `state`, a variable or `}`")
		}
	}
}

// state_decl: INITIAL? "state" NAME "{" (_NL | state_item)* "}"
// state_item: state_enter | state_exit | state_on _NL | state_after _NL
//
//	| become_stmt _NL | statement
func (p *parser) stateDecl(initial bool) *StateDecl {
	name := p.name()
	s := &StateDecl{Base: Base{p.region(name)}, Name: name.text, IsInitial: initial}
	p.expectSym("{")
	for {
		t := p.peek(ctxStmt)
		switch {
		case t.kind == tNL:
			p.take(t)
		case t.sym("}"):
			p.take(t)
			return s
		case t.word("enter"):
			p.take(t)
			s.EnterBody = p.block()
		case t.word("exit"):
			p.take(t)
			s.ExitBody = p.block()
		case t.word("on"):
			// state_on: "on" expr state_guard? "->" NAME state_action?
			// shares its prefix with on_block.
			p.take(t)
			event := p.expr()
			if n := p.peek(ctxOp); n.sym("[") || n.sym("->") {
				guard, target, action := p.transitionTail()
				s.Transitions = append(s.Transitions, &StateTransition{
					Base{regionOf(event)}, event, target, guard, action,
				})
				p.expectNL()
			} else {
				s.Body = append(s.Body, p.onBlock(event))
			}
		case t.word("after"):
			// state_after: "after" expr state_guard? "->" NAME state_action?
			p.take(t)
			d := p.expr()
			guard, target, action := p.transitionTail()
			s.TimedTransitions = append(s.TimedTransitions, &TimedTransition{
				Base{regionOf(d)}, d, target, guard, action,
			})
			p.expectNL()
		case t.word("become"):
			// Parsed but dropped: parser.py keeps only the statement
			// types listed below in a state body.
			p.take(t)
			p.name()
			p.expectNL()
		default:
			stmt, simple := p.statement()
			if simple {
				p.expectNL()
			}
			switch stmt.(type) {
			case *EveryBlock, *OnBlock, *ExprStmt, *VarDecl, *IfStmt, *WhileStmt, *ForStmt, *AssignExpr:
				s.Body = append(s.Body, stmt)
			}
		}
	}
}

// transitionTail parses state_guard? "->" NAME state_action?
// state_guard: "[" expr "]";  state_action: block
func (p *parser) transitionTail() (guard Node, target string, action []Node) {
	if t := p.peek(ctxName); t.sym("[") {
		p.take(t)
		guard = p.expr()
		p.expectSym("]")
	}
	p.expectSym("->")
	target = p.name().text
	if t := p.peek(ctxName); t.sym("{") {
		action = p.block()
	}
	return
}

// --- Top-level declarations ---

// storage_decl: "storage" NAME ":" type_name "=" expr
func (p *parser) storageDecl() *StorageDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	p.expectSym(":")
	typ := p.typeName()
	p.expectSym("=")
	return &StorageDecl{Base{p.region(name)}, name.text, typ, p.expr()}
}

// channel_decl: "channel" NAME ":" type_name ("[" INT "]")?
func (p *parser) channelDecl() *ChannelDecl {
	p.take(p.peek(ctxTop))
	name := p.name()
	p.expectSym(":")
	c := &ChannelDecl{Base: Base{p.region(name)}, Name: name.text, ElemType: p.typeName(), Capacity: 4}
	if t := p.peek(ctxName); t.sym("[") {
		p.take(t)
		n := p.next(ctxName)
		if n.kind != tInt {
			p.unexpectedWant(n, "an integer capacity")
		}
		c.Capacity = p.atoi(n.text)
		p.expectSym("]")
	}
	return c
}

// type_name: NAME | NAME "<" type_name ">" | NAME "<" INT ">"
// Types are kept as their source spelling, e.g. "list<int>".
func (p *parser) typeName() string {
	name := p.name().text
	if t := p.peek(ctxType); !t.sym("<") {
		return name
	} else {
		p.take(t)
	}
	var arg string
	if t := p.peek(ctxType); t.kind == tInt {
		arg = p.take(t).text
	} else {
		arg = p.typeName()
	}
	p.expectSymIn(ctxType, ">")
	return name + "<" + arg + ">"
}

// var_decl: VAR_KEYWORD NAME type_annotation? "=" expr
//
//	| "shared" VAR_KEYWORD NAME type_annotation? "=" expr
//
// ctx is the context the caller peeked the first token with.
func (p *parser) varDecl(ctx lexCtx) *VarDecl {
	t := p.next(ctx)
	shared := t.word("shared")
	if shared {
		t = p.next(lexCtx{varKw: true})
	}
	if t.kind != tVarKw {
		p.unexpectedWant(t, "`let` or `var`")
	}
	name := p.name()
	v := &VarDecl{Base: Base{p.region(name)}, Name: name.text, Mutable: t.text == "var", Shared: shared}
	if c := p.peek(ctxName); c.sym(":") {
		p.take(c)
		typ := p.typeName()
		v.TypeName = &typ
	}
	p.expectSym("=")
	v.Value = p.expr()
	return v
}

// --- Blocks and statements ---

// block: "{" (_NL | statement)* _inline_stmt? "}"
// _inline_stmt: a simple statement (or become_stmt) right before the "}".
func (p *parser) block() []Node {
	p.expectSym("{")
	body := []Node{}
	for {
		t := p.peek(ctxStmt)
		switch {
		case t.kind == tNL:
			p.take(t)
			continue
		case t.sym("}"):
			p.take(t)
			return body
		case t.word("become"):
			// become_stmt is only an _inline_stmt in a block.
			p.take(t)
			name := p.name()
			body = append(body, &BecomeStmt{Base{p.region(name)}, name.text})
			p.expectSym("}")
			return body
		}
		stmt, simple := p.statement()
		body = append(body, stmt)
		if simple {
			switch t := p.next(ctxName); {
			case t.kind == tNL:
			case t.sym("}"):
				return body
			default:
				p.unexpectedWant(t, "a statement or `}`")
			}
		}
	}
}

// statement parses one statement at statement start. simple reports a
// statement that must be followed by _NL (or, in a block, the "}").
func (p *parser) statement() (stmt Node, simple bool) {
	t := p.peek(ctxStmt)
	if t.kind == tVarKw || t.word("shared") {
		return p.varDecl(ctxStmt), true
	}
	switch {
	case t.word("return"):
		// return_stmt: "return" expr?
		p.take(t)
		r := &ReturnStmt{}
		if n := p.peek(ctxName); n.kind != tNL && !n.sym("}") {
			r.Value = p.expr()
		}
		return r, true
	case t.word("break"):
		p.take(t)
		return &BreakStmt{}, true
	case t.word("continue"):
		p.take(t)
		return &ContinueStmt{}, true
	case t.word("if"):
		p.take(t)
		return p.ifStmt(), false
	case t.word("while"):
		// while_stmt: "while" expr block
		p.take(t)
		cond := p.expr()
		return &WhileStmt{Base{regionOf(cond)}, cond, p.block()}, false
	case t.word("for"):
		p.take(t)
		return p.forStmt(), false
	case t.word("loop"):
		// loop_stmt: "loop" block — an infinite while.
		p.take(t)
		return &WhileStmt{Condition: &BoolLiteral{Value: true}, Body: p.block()}, false
	case t.word("match"):
		p.take(t)
		return p.matchStmt(), false
	case t.word("every"):
		p.take(t)
		return p.everyBlock(), false
	case t.word("on"):
		p.take(t)
		return p.onBlock(p.expr()), false
	case t.word("timeout"):
		p.take(t)
		return p.timeoutBlock(), false
	case t.word("try"):
		p.take(t)
		return p.tryBlock(), false
	}
	return p.exprStmt(), true
}

// expr_stmt: expr  |  assign_stmt: postfix ASSIGN_OP expr
// An expression statement that is an assignment is the AssignExpr itself.
func (p *parser) exprStmt() Node {
	left, bare := p.unary()
	if bare {
		t := p.peek(ctxOpAssign)
		if t.kind == tAssignOp {
			p.take(t)
			return &CompoundAssign{Base{regionOf(left)}, left, t.text, p.expr()}
		}
	}
	e := p.assignTail(left, bare)
	if a, ok := e.(*AssignExpr); ok {
		return a
	}
	return &ExprStmt{Expr: e}
}

// if_stmt: "if" expr block else_clause?
// else_clause: "else" if_stmt | "else" block
func (p *parser) ifStmt() *IfStmt {
	cond := p.expr()
	s := &IfStmt{Base: Base{regionOf(cond)}, Condition: cond, ThenBody: p.block()}
	if t := p.peek(lexCtx{name: true, varKw: true, elseKw: true}); t.word("else") {
		p.take(t)
		if t := p.peek(prefixCtx("if")); t.word("if") {
			p.take(t)
			s.ElseBody = p.ifStmt()
		} else {
			s.ElseBody = p.block()
		}
	}
	return s
}

// for_stmt: "for" NAME "in" expr ".." expr step_clause? block
// step_clause: "step" expr
func (p *parser) forStmt() *ForStmt {
	name := p.name()
	f := &ForStmt{Base: Base{p.region(name)}, VarName: name.text}
	p.expectKw("in")
	f.Start = p.expr()
	p.expectSym("..")
	f.End = p.expr()
	if t := p.peek(prefixCtx("step")); t.word("step") {
		p.take(t)
		f.Step = p.expr()
	}
	f.Body = p.block()
	return f
}

// every_block: "every" expr STRICT? when_clause? block (after "every")
// when_clause: "when" expr
func (p *parser) everyBlock() *EveryBlock {
	interval := p.expr()
	e := &EveryBlock{Base: Base{regionOf(interval)}, Interval: interval}
	if t := p.peek(prefixCtx("strict", "when")); t.word("strict") {
		p.take(t)
		e.Strict = true
	}
	if t := p.peek(prefixCtx("when")); t.word("when") {
		p.take(t)
		e.Guard = p.expr()
	}
	e.Body = p.block()
	return e
}

// on_block: "on" expr debounce_clause? throttle_clause? block
// (called after "on" expr)
func (p *parser) onBlock(event Node) *OnBlock {
	o := &OnBlock{Base: Base{regionOf(event)}, Event: event}
	if t := p.peek(prefixCtx("debounce", "throttle")); t.word("debounce") {
		p.take(t)
		o.Debounce = p.expr()
	}
	if t := p.peek(prefixCtx("throttle")); t.word("throttle") {
		p.take(t)
		o.Throttle = p.expr()
	}
	o.Body = p.block()
	return o
}

// timeout_block: "timeout" expr block TIMEOUT_ELSE? block
func (p *parser) timeoutBlock() *TimeoutBlock {
	d := p.expr()
	b := &TimeoutBlock{Base: Base{regionOf(d)}, Duration: d, TryBody: p.block()}
	if t := p.peek(lexCtx{elseKw: true}); t.word("else") {
		p.take(t)
	}
	b.FallbackBody = p.block()
	return b
}

// try_block: "try" block try_or_clause
// try_or_clause: "or" "retry" [INT "times" "else" terminal_policy]
//
//	| "or" "skip" | "or" "restart"
//
// terminal_policy: "skip" | "restart" | "fail"
func (p *parser) tryBlock() *TryBlock {
	body := p.block()
	if len(body) == 0 {
		// parser.py asserts; the exception becomes a syntax error.
		p.failNear("try block body must contain at least one statement")
	}
	b := &TryBlock{Base: Base{regionOf(body[0])}, Body: body}
	p.expectKw("or")
	t := p.next(prefixCtx("retry", "skip", "restart"))
	switch {
	case t.word("retry"):
		b.Policy = "retry"
		if n := p.peek(ctxStmt); n.kind == tInt {
			p.take(n)
			limit := p.atoi(n.text)
			b.RetryLimit = &limit
			p.expectKw("times")
			if e := p.next(lexCtx{elseKw: true}); !e.word("else") {
				p.unexpectedWant(e, "`else`")
			}
			term := p.next(prefixCtx("skip", "restart", "fail"))
			if !term.word("skip") && !term.word("restart") && !term.word("fail") {
				p.unexpectedWant(term, "`skip`, `restart` or `fail`")
			}
			b.Terminal = &term.text
		}
	case t.word("skip"), t.word("restart"):
		b.Policy = t.text
	default:
		p.unexpectedWant(t, "`retry`, `skip` or `restart`")
	}
	return b
}

// match_stmt: "match" "(" expr ")" "{" (_NL | match_arm)* "}"
// match_arm: match_pattern "->" match_body _NL
// match_pattern: "." NAME | INT | INT ".." INT | "_"
// match_body: expr | block
func (p *parser) matchStmt() *MatchStmt {
	p.expectSym("(")
	e := p.expr()
	p.expectSym(")")
	m := &MatchStmt{Base: Base{regionOf(e)}, Expr: e}
	p.expectSym("{")
	for {
		t := p.next(prefixCtx("_"))
		arm := &MatchArm{}
		switch {
		case t.kind == tNL:
			continue
		case t.sym("}"):
			return m
		case t.sym("."):
			arm.Pattern = p.name().text
		case t.kind == tInt:
			arm.Pattern = t.text
			if n := p.peek(ctxName); n.sym("..") {
				p.take(n)
				hi := p.next(ctxName)
				if hi.kind != tInt {
					p.unexpectedWant(hi, "an integer")
				}
				arm.Pattern += ".." + hi.text
			}
		case t.word("_"):
			arm.Pattern = "_"
		default:
			p.unexpectedWant(t, "a pattern (number, name or `_`)")
		}
		p.expectSym("->")
		if t := p.peek(ctxName); t.sym("{") {
			arm.Body = p.block()
		} else {
			arm.Body = p.expr()
		}
		p.expectNL()
		m.Arms = append(m.Arms, arm)
	}
}

// --- Expressions ---

// Binary operator precedence, lowest first; all left-associative.
var binaryPrec = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3, "<=": 3, ">=": 3, "<": 3, ">": 3,
	"|":  4,
	"^":  5,
	"&":  6,
	"<<": 7, ">>": 7,
	"+": 8, "-": 8,
	"*": 9, "/": 9, "%": 9,
}

// expr: assign_expr
// assign_expr: or_expr | postfix "=" or_expr -> assign_op
func (p *parser) expr() Node {
	return p.assignTail(p.unary())
}

// assignTail continues an expression whose first unary operand is parsed.
// bare reports that operand was a plain postfix (the only valid target).
func (p *parser) assignTail(left Node, bare bool) Node {
	if bare {
		if t := p.peek(ctxOp); t.sym("=") {
			p.take(t)
			return &AssignExpr{Base{regionOf(left)}, left, p.binary(1, nil)}
		}
	}
	return p.binary(1, left)
}

// binary parses operators of precedence >= minPrec by precedence climbing,
// starting from an already parsed left operand if one is given.
func (p *parser) binary(minPrec int, left Node) Node {
	if left == nil {
		left, _ = p.unary()
	}
	for {
		t := p.peek(ctxOp)
		prec, ok := binaryPrec[t.text]
		if t.kind != tSym || !ok || prec < minPrec {
			return left
		}
		p.take(t)
		right := p.binary(prec+1, nil)
		left = &BinaryExpr{Base: Base{regionOf(left)}, Left: left, Op: t.text, Right: right}
	}
}

// unary: "!" unary | "-" unary | "~" unary | postfix
func (p *parser) unary() (n Node, bare bool) {
	t := p.peek(ctxName)
	if t.sym("!") || t.sym("-") || t.sym("~") {
		p.take(t)
		operand, _ := p.unary()
		return &UnaryExpr{Base{regionOf(operand)}, t.text, operand}, false
	}
	return p.postfix(), true
}

// postfix: primary | postfix "." NAME | postfix "(" arg_list? ")"
//
//	| postfix "or" or_fallback
//
// or_fallback: "skip" | "restart" | "retry" "(" expr ")" | expr
func (p *parser) postfix() Node {
	n := p.primary()
	for {
		t := p.peek(ctxOp)
		switch {
		case t.sym("."):
			p.take(t)
			n = &DotAccess{Base{regionOf(n)}, n, p.name().text}
		case t.sym("("):
			p.take(t)
			args := p.argList(true)
			switch c := n.(type) {
			case *DotAccess:
				n = &MethodCall{Base{c.Region}, c.Obj, c.FieldName, args}
			case *VarRef:
				n = &FuncCall{Base{c.Region}, c.Name, args}
			default: // Python: FuncCall(name=str(callee), ...)
				n = &FuncCall{Base{regionOf(n)}, pyRepr(reflect.ValueOf(n)), args}
			}
		case t.word("or"):
			p.take(t)
			o := &OrExpr{Base: Base{regionOf(n)}, Expr: n}
			switch f := p.peek(ctxName); {
			case f.word("skip"), f.word("restart"):
				p.take(f)
				o.Kind = f.text
			case f.word("retry"):
				p.take(f)
				p.expectSym("(")
				o.Kind, o.Fallback = "retry", p.expr()
				p.expectSym(")")
			default:
				o.Kind, o.Fallback = "default", p.expr()
			}
			n = o
		default:
			return n
		}
	}
}

// argList parses arg_list ")" after the "(": expr ("," expr)*.
// optional allows an empty list.
func (p *parser) argList(optional bool) []Node {
	args := []Node{}
	if t := p.peek(ctxName); optional && t.sym(")") {
		p.take(t)
		return args
	}
	for {
		args = append(args, p.expr())
		t := p.next(ctxName)
		if t.sym(")") {
			return args
		}
		if !t.sym(",") {
			p.unexpectedWant(t, "`,` or `)`")
		}
	}
}

// primary: DURATION | FLOAT | HEX_INT | BIN_INT | INT | ESCAPED_STRING
//
//	| "true" | "false" | NAME | "(" expr ")" | array_literal
func (p *parser) primary() Node {
	t := p.next(ctxName)
	r := Base{p.region(t)}
	switch t.kind {
	case tDuration:
		return &DurationLiteral{r, parseDuration(t.text), t.text}
	case tFloat:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			p.fail(t, "invalid float %q", t.text)
		}
		return &FloatLiteral{r, f}
	case tHex:
		return &IntLiteral{r, p.parseInt(t, t.text[2:], 16)}
	case tBin:
		// int(text.replace("_", ""), 2) fails on "0b_".
		return &IntLiteral{r, p.parseInt(t, strings.ReplaceAll(t.text[2:], "_", ""), 2)}
	case tInt:
		return &IntLiteral{r, p.parseInt(t, t.text, 10)}
	case tString:
		return &StringLiteral{r, t.text[1 : len(t.text)-1]}
	case tName:
		switch t.text {
		case "true":
			return &BoolLiteral{Value: true}
		case "false":
			return &BoolLiteral{Value: false}
		}
		return &VarRef{r, t.text}
	case tSym:
		switch t.text {
		case "(":
			e := p.expr()
			p.expectSym(")")
			return e
		case "[":
			// array_literal: "[" (expr ("," expr)*)? "]"
			a := &ArrayLiteral{Elements: []Node{}}
			if c := p.peek(ctxName); c.sym("]") {
				p.take(c)
				return a
			}
			for {
				a.Elements = append(a.Elements, p.expr())
				c := p.next(ctxName)
				if c.sym("]") {
					break
				}
				if !c.sym(",") {
					p.unexpectedWant(c, "`,` or `]`")
				}
			}
			first, last := regionOf(a.Elements[0]), regionOf(a.Elements[len(a.Elements)-1])
			a.Region = Region{first.StartLine, first.StartCol, last.EndLine, last.EndCol}
			return a
		}
	}
	p.unexpectedWant(t, "an expression")
	return nil
}

func (p *parser) parseInt(t token, digits string, base int) int {
	n, err := strconv.ParseInt(digits, base, 64)
	if err != nil {
		p.fail(t, "invalid integer %q", t.text)
	}
	return int(n)
}

func (p *parser) atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		p.failNear("invalid integer %q", s)
	}
	return n
}

// durationUnits in the order parser.py tries them: longest first, ties in
// the order of its _DURATION_TO_US dict.
var durationUnits = []struct {
	unit string
	us   int
}{
	{"seconds", 1_000_000}, {"minutes", 60_000_000}, {"second", 1_000_000}, {"minute", 60_000_000},
	{"hours", 3_600_000_000}, {"hour", 3_600_000_000}, {"sec", 1_000_000}, {"min", 60_000_000},
	{"us", 1}, {"ms", 1000}, {"hr", 3_600_000_000}, {"s", 1_000_000},
}

// parseDuration converts a DURATION token such as "500ms" or "1.5s" to
// microseconds, truncating fractions as Python's int() does.
func parseDuration(raw string) int {
	for _, u := range durationUnits {
		num, ok := strings.CutSuffix(raw, u.unit)
		if !ok {
			continue
		}
		if strings.Contains(num, ".") {
			f, _ := strconv.ParseFloat(num, 64)
			return int(f * float64(u.us))
		}
		n, _ := strconv.Atoi(num)
		return n * u.us
	}
	return 0
}
