// Package ide is the Volt IDE worker's ops (ported from the Python
// worker.py): diagnostics and side-panel data, formatting, symbols, state
// diagrams and the system graph. Results marshal to the JSON worker.py used.
package ide

import (
	"github.com/PhilemonChiro/volt-compiler"
)

type Diagnostic struct {
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Hint     string `json:"hint"`
	EndLine  int    `json:"end_line,omitempty"`
	EndCol   int    `json:"end_col,omitempty"`
	Code     string `json:"code,omitempty"`
	Fixes    []Fix  `json:"fixes,omitempty"`
}

// Fix is a suggested edit: replace line:col..end_line:end_col with text.
type Fix struct {
	Title   string `json:"title"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"end_line"`
	EndCol  int    `json:"end_col"`
	Text    string `json:"text"`
}

type Pin struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	GPIO int    `json:"gpio"`
}

type Device struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Bus     string `json:"bus"`
	Address int    `json:"address"`
}

type Bus struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Task struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
	Line int    `json:"line"`
}

type Machine struct {
	Name   string   `json:"name"`
	States []string `json:"states"`
	Line   int      `json:"line"`
}

type Channel struct {
	Name     string `json:"name"`
	ElemType string `json:"elem_type"`
	Capacity int    `json:"capacity"`
}

type Enum struct {
	Name     string   `json:"name"`
	Variants []string `json:"variants"`
}

type CompileResult struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	CCode       string       `json:"c_code"`
	Pins        []Pin        `json:"pins"`
	Devices     []Device     `json:"devices"`
	Buses       []Bus        `json:"buses"`
	Tasks       []Task       `json:"tasks"`
	Machines    []Machine    `json:"machines"`
	Channels    []Channel    `json:"channels"`
	Enums       []Enum       `json:"enums"`
	Chip        string       `json:"chip"`
}

// CompileFull compiles for diagnostics and C, then re-parses to extract the
// side-panel data. Each re-run pass is guarded on its own (a failing pass
// keeps what earlier ones produced) and, unlike Compile, runs even after
// errors and without volt.toml's chip.
func CompileFull(source, filename string, progress compiler.ProgressFunc) (*CompileResult, error) {
	code, errs, err := compiler.CompileProgress(source, filename, progress)
	if err != nil {
		return nil, err
	}
	res := &CompileResult{
		Diagnostics: []Diagnostic{}, CCode: code,
		Pins: []Pin{}, Devices: []Device{}, Buses: []Bus{}, Tasks: []Task{},
		Machines: []Machine{}, Channels: []Channel{}, Enums: []Enum{},
	}
	for _, d := range errs.List {
		r := d.Region
		dd := Diagnostic{Line: r.StartLine, Col: r.StartCol, Message: d.Message, Severity: d.Severity, Hint: d.Hint,
			EndLine: r.EndLine, EndCol: r.EndCol, Code: d.Code}
		for _, f := range d.Fixes {
			fr := f.Region
			dd.Fixes = append(dd.Fixes, Fix{f.Title, fr.StartLine, fr.StartCol, fr.EndLine, fr.EndCol, f.Text})
		}
		res.Diagnostics = append(res.Diagnostics, dd)
	}

	prog, err := compiler.Parse(source)
	if err != nil {
		return res, nil
	}
	errs2 := &compiler.Errors{}
	errs2.SetSource(source)
	var scope *compiler.Scope
	guard(func() { scope = compiler.Resolve(prog, errs2) })
	if scope != nil {
		guard(func() { compiler.CheckTypes(prog, scope, errs2) })
		guard(func() { compiler.Analyze(prog, scope, errs2) })
	}

	if prog.Chip != nil {
		res.Chip = prog.Chip.Target
	}
	if hw := prog.Hardware; hw != nil {
		for _, p := range hw.Pins {
			res.Pins = append(res.Pins, Pin{p.Name, p.Mode, p.GPIO})
		}
		for _, s := range hw.Sensors {
			res.Devices = append(res.Devices, Device{s.Name, s.SensorType, s.BusName, s.Address})
		}
		for _, b := range hw.Buses {
			res.Buses = append(res.Buses, Bus{b.Name, b.BusType})
		}
	}
	for _, t := range prog.Tasks {
		res.Tasks = append(res.Tasks, Task{t.Name, t.Tier, t.Region.StartLine})
	}
	for _, m := range prog.Machines {
		states := []string{}
		for _, s := range m.States {
			states = append(states, s.Name)
		}
		res.Machines = append(res.Machines, Machine{m.Name, states, m.Region.StartLine})
	}
	for _, c := range prog.Channels {
		res.Channels = append(res.Channels, Channel{c.Name, c.ElemType, c.Capacity})
	}
	for _, e := range prog.Enums {
		res.Enums = append(res.Enums, Enum{e.Name, nonNil(e.Variants)})
	}
	return res, nil
}

// guard runs a pass, swallowing a panic as worker.py swallows exceptions.
func guard(f func()) {
	defer func() { _ = recover() }()
	f()
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// FormatCode formats source (worker.py format_code).
func FormatCode(source string) (string, error) { return compiler.Format(source) }

type Ref struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type Symbol struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	DefLine int    `json:"def_line"`
	DefCol  int    `json:"def_col"`
	Refs    []Ref  `json:"refs"`
}

// FindSymbols lists declarations and their references. Keys are symbol
// names (states: "machine.state"); a later declaration of the same name
// replaces the earlier one in place.
func FindSymbols(source string) []*Symbol {
	prog, err := compiler.Parse(source)
	if err != nil {
		return []*Symbol{}
	}
	errs := &compiler.Errors{}
	errs.SetSource(source)
	guard(func() { compiler.Resolve(prog, errs) })

	var syms ordered[*Symbol]
	add := func(key, name, kind string, r compiler.Region) {
		syms.set(key, &Symbol{name, kind, r.StartLine, r.StartCol, []Ref{}})
	}
	if hw := prog.Hardware; hw != nil {
		for _, p := range hw.Pins {
			add(p.Name, p.Name, "pin", p.Region)
		}
		for _, s := range hw.Sensors {
			add(s.Name, s.Name, "sensor", s.Region)
		}
		for _, b := range hw.Buses {
			add(b.Name, b.Name, "bus", b.Region)
		}
	}
	for _, f := range prog.Functions {
		add(f.Name, f.Name, "func", f.Region)
		for _, p := range f.Params {
			add(p.Name, p.Name, "param", p.Region)
		}
	}
	for _, t := range prog.Tasks {
		add(t.Name, t.Name, "task", t.Region)
	}
	for _, m := range prog.Machines {
		add(m.Name, m.Name, "machine", m.Region)
		for _, s := range m.States {
			add(m.Name+"."+s.Name, s.Name, "state", s.Region)
		}
	}
	for _, sv := range prog.StorageVars {
		add(sv.Name, sv.Name, "storage", sv.Region)
	}
	for _, gv := range prog.GlobalVars {
		add(gv.Name, gv.Name, "var", gv.Region)
	}

	ref := func(name string, r compiler.Region) {
		if s, ok := syms.get(name); ok {
			s.Refs = append(s.Refs, Ref{r.StartLine, r.StartCol})
		}
	}
	var walkExpr func(e compiler.Node)
	walkExpr = func(e compiler.Node) {
		switch e := e.(type) {
		case *compiler.VarRef:
			ref(e.Name, e.Region)
		case *compiler.BinaryExpr:
			walkExpr(e.Left)
			walkExpr(e.Right)
		case *compiler.UnaryExpr:
			walkExpr(e.Operand)
		case *compiler.DotAccess:
			walkExpr(e.Obj)
		case *compiler.FuncCall:
			ref(e.Name, e.Region)
			for _, a := range e.Args {
				walkExpr(a)
			}
		case *compiler.MethodCall:
			walkExpr(e.Obj)
			for _, a := range e.Args {
				walkExpr(a)
			}
		case *compiler.AssignExpr:
			walkExpr(e.Target)
			walkExpr(e.Value)
		}
	}
	var walkStmts func([]compiler.Node)
	walkStmts = func(stmts []compiler.Node) {
		for _, s := range stmts {
			switch s := s.(type) {
			case *compiler.VarDecl:
				if _, ok := syms.get(s.Name); !ok {
					add(s.Name, s.Name, "var", s.Region)
				}
				walkExpr(s.Value)
			case *compiler.AssignExpr:
				walkExpr(s.Target)
				walkExpr(s.Value)
			case *compiler.CompoundAssign:
				walkExpr(s.Target)
				walkExpr(s.Value)
			case *compiler.ExprStmt:
				walkExpr(s.Expr)
			case *compiler.ReturnStmt:
				walkExpr(s.Value)
			case *compiler.IfStmt:
				walkExpr(s.Condition)
				walkStmts(s.ThenBody)
				switch e := s.ElseBody.(type) {
				case []compiler.Node:
					walkStmts(e)
				case *compiler.IfStmt:
					if e != nil {
						walkStmts([]compiler.Node{e})
					}
				}
			case *compiler.WhileStmt:
				walkExpr(s.Condition)
				walkStmts(s.Body)
			case *compiler.ForStmt:
				walkExpr(s.Start)
				walkExpr(s.End)
				walkStmts(s.Body)
			case *compiler.EveryBlock:
				walkStmts(s.Body)
			case *compiler.OnBlock:
				walkExpr(s.Event)
				walkStmts(s.Body)
			case *compiler.TimeoutBlock:
				walkStmts(s.TryBody)
				walkStmts(s.FallbackBody)
			case *compiler.MatchStmt:
				walkExpr(s.Expr)
			}
		}
	}
	for _, f := range prog.Functions {
		walkStmts(f.Body)
	}
	for _, t := range prog.Tasks {
		walkStmts(t.Body)
	}
	for _, m := range prog.Machines {
		for _, s := range m.States {
			walkStmts(s.EnterBody)
			walkStmts(s.ExitBody)
			walkStmts(s.Body)
		}
	}
	if prog.Setup != nil {
		walkStmts(prog.Setup.Body)
	}
	if prog.Loop != nil {
		walkStmts(prog.Loop.Body)
	}

	out := make([]*Symbol, len(syms.keys))
	for i, k := range syms.keys {
		out[i] = syms.vals[k]
	}
	return out
}
