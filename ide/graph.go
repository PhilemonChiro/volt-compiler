package ide

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PhilemonChiro/volt-compiler"
)

type Unit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Line int    `json:"line"`
}

type EdgeAnim struct {
	Drive   string
	Src     string
	Dst     string
	RateMS  *float64 // timer drives only; nil = null
	hasRate bool
}

// MarshalJSON writes rate_ms (possibly null) only for timer drives.
func (e EdgeAnim) MarshalJSON() ([]byte, error) {
	var m ordered[any]
	m.set("drive", e.Drive)
	m.set("src", e.Src)
	m.set("dst", e.Dst)
	if e.hasRate {
		m.set("rate_ms", e.RateMS)
	}
	return m.MarshalJSON()
}

type NodeInfo struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Line   *int   `json:"line"`
	Detail string `json:"detail"`
}

// SystemGraphResult: when the source does not parse, only Mermaid ("")
// and Units ([]) are set and only they are marshalled.
type SystemGraphResult struct {
	Mermaid string
	Units   []Unit
	Loops   []string
	Edges   []EdgeAnim
	Nodes   *ordered[*NodeInfo]
}

func (r SystemGraphResult) MarshalJSON() ([]byte, error) {
	var m ordered[any]
	m.set("mermaid", r.Mermaid)
	m.set("units", nonNil(r.Units))
	if r.Nodes != nil {
		m.set("loops", nonNil(r.Loops))
		m.set("edges", nonNil(r.Edges))
		m.set("nodes", r.Nodes)
	}
	return m.MarshalJSON()
}

type gnode struct {
	label, kind string
	rate, name  string // timers only
}

type gedge struct{ src, dst, kind, label string }

var readMethods = map[string]bool{"read": true, "value": true, "get": true, "receive": true,
	"has_data": true, "peek": true, "pop": true, "available": true}

// isReadMethod: a read/query returns data to the caller, so its edge flows
// resource→unit.
func isReadMethod(m string) bool {
	return readMethods[m] || strings.HasPrefix(m, "is_") || strings.HasPrefix(m, "has_") ||
		strings.HasPrefix(m, "get_")
}

var shapes = map[string][2]string{
	"task":    {`["`, `"]`},
	"timer":   {`(("`, `"))`}, // circle → periodic/cyclic source
	"machine": {`[["`, `"]]`},
	"func":    {`("`, `")`},
	"channel": {`[/"`, `"/]`},
	"var":     {`("`, `")`},
	"pin":     {`(["`, `"])`},
	"service": {`{{"`, `"}}`},
}

var classDefs = []string{
	"    classDef task fill:#1f3a4d,stroke:#2B7CB3,color:#e8e8e8;",
	"    classDef timer fill:#33291a,stroke:#d0a83a,color:#f5e9cf;",
	"    classDef machine fill:#2d1f3d,stroke:#7a3ac0,color:#e8e8e8;",
	"    classDef func fill:#26333a,stroke:#5a8fa0,color:#e8e8e8;",
	"    classDef channel fill:#3a2d1f,stroke:#c08a3a,color:#e8e8e8;",
	"    classDef var fill:#2a2d33,stroke:#8a8f98,color:#e8e8e8;",
	"    classDef pin fill:#1f3d2a,stroke:#3aa06a,color:#e8e8e8;",
	"    classDef service fill:#33262f,stroke:#a03a6a,color:#e8e8e8;",
	"    classDef shared fill:#3d2a1f,stroke:#d08a3a,stroke-width:2.5px,color:#f0e0c0;",
}

// SystemGraph builds the whole-program causal map: concurrent units
// (setup, tasks, machines, functions) and the resources they touch, as a
// Mermaid flowchart. Loops (timer tasks) not in expanded fold the
// resources only they touch.
func SystemGraph(source string, expanded []string) SystemGraphResult {
	prog, err := compiler.Parse(source)
	if err != nil {
		return SystemGraphResult{Units: []Unit{}}
	}
	src := newSnipper(source)

	var hwPins []*compiler.PinDecl
	if prog.Hardware != nil {
		hwPins = prog.Hardware.Pins
	}
	pins, channels, globals, funcs := set{}, set{}, set{}, set{}
	pinDecls := map[string]*compiler.PinDecl{}
	chanDecls := map[string]*compiler.ChannelDecl{}
	varDecls := map[string]*compiler.VarDecl{}
	storageDecls := map[string]*compiler.StorageDecl{}
	taskDecls := map[string]*compiler.TaskDecl{}
	machineDecls := map[string]*compiler.MachineDecl{}
	funcDecls := map[string]*compiler.FuncDecl{}
	for _, p := range hwPins {
		pins[p.Name], pinDecls[p.Name] = true, p
	}
	for _, c := range prog.Channels {
		channels[c.Name], chanDecls[c.Name] = true, c
	}
	for _, v := range prog.GlobalVars {
		globals[v.Name], varDecls[v.Name] = true, v
	}
	for _, s := range prog.StorageVars {
		globals[s.Name], storageDecls[s.Name] = true, s
	}
	for _, f := range prog.Functions {
		funcs[f.Name], funcDecls[f.Name] = true, f
	}
	for _, t := range prog.Tasks {
		taskDecls[t.Name] = t
	}
	for _, m := range prog.Machines {
		machineDecls[m.Name] = m
	}

	var nodes ordered[*gnode]
	var edges ordered[gedge]
	units := []Unit{}
	node := func(id, label, kind string) {
		if _, ok := nodes.get(id); !ok {
			nodes.set(id, &gnode{label: label, kind: kind})
		}
	}
	edge := func(src, dst, kind, label string) {
		e := gedge{src, dst, kind, label}
		edges.set(src+"\x00"+dst+"\x00"+kind+"\x00"+label, e)
	}

	var walk func(n any, unit string)
	walk = func(n any, unit string) {
		if isNil(n) {
			return
		}
		if items, ok := list(n); ok {
			for _, x := range items {
				walk(x, unit)
			}
			return
		}
		switch n := n.(type) {
		case *compiler.AssignExpr:
			walkAssign(n.Target, n.Value, unit, globals, node, edge, walk)
			return
		case *compiler.CompoundAssign:
			walkAssign(n.Target, n.Value, unit, globals, node, edge, walk)
			return
		case *compiler.MethodCall:
			m := n.Method
			if v, ok := n.Obj.(*compiler.VarRef); ok && v != nil {
				recv := v.Name
				switch {
				case channels[recv]:
					node("c_"+recv, recv, "channel")
					if isReadMethod(m) {
						edge("c_"+recv, unit, "consume", m)
					} else {
						edge(unit, "c_"+recv, "produce", m)
					}
				case pins[recv]:
					node("p_"+recv, recv, "pin")
					if isReadMethod(m) {
						edge("p_"+recv, unit, "in", m)
					} else {
						edge(unit, "p_"+recv, "out", m)
					}
				case !globals[recv]:
					node("s_"+recv, recv, "service")
					if isReadMethod(m) {
						edge("s_"+recv, unit, "in", m)
					} else {
						edge(unit, "s_"+recv, "service", m)
					}
				}
			}
			walk(n.Args, unit)
			return
		case *compiler.FuncCall:
			if funcs[n.Name] {
				node("f_"+n.Name, n.Name+"()", "func")
				edge(unit, "f_"+n.Name, "call", "call")
			} else {
				node("s_"+n.Name, n.Name, "service")
				edge(unit, "s_"+n.Name, "service", n.Name)
			}
			walk(n.Args, unit)
			return
		case *compiler.VarRef:
			if globals[n.Name] {
				node("v_"+n.Name, n.Name, "var")
				edge("v_"+n.Name, unit, "read", "read")
			}
			return
		}
		children(n, func(v any) { walk(v, unit) })
	}

	// firstInterval: source text of the first `every` interval in body;
	// the search stops at the first EveryBlock even if its text is empty.
	firstInterval := func(body []compiler.Node) string {
		found, iv := false, ""
		var scan func(any)
		scan = func(n any) {
			if found || isNil(n) {
				return
			}
			if items, ok := list(n); ok {
				for _, x := range items {
					scan(x)
				}
				return
			}
			if e, ok := n.(*compiler.EveryBlock); ok {
				found, iv = true, src.snip(regionOf(e.Interval))
				return
			}
			children(n, scan)
		}
		scan(body)
		return iv
	}

	addUnit := func(id, label, kind string, body []compiler.Node, r compiler.Region) {
		node(id, label, kind)
		units = append(units, Unit{id, label, r.StartLine})
		walk(body, id)
	}

	if prog.Setup != nil {
		addUnit("u_setup", "setup", "task", prog.Setup.Body, prog.Setup.Region)
	}
	for _, t := range prog.Tasks {
		if iv := firstInterval(t.Body); iv != "" {
			// Periodic source: circle shape + rate-forward label.
			disp := t.Name
			if strings.HasPrefix(t.Name, "_rule") {
				disp = ""
			}
			lbl := "↻ " + iv
			if disp != "" {
				lbl = disp + "<br>↻ " + iv
			}
			addUnit("u_"+t.Name, lbl, "timer", t.Body, t.Region)
			n, _ := nodes.get("u_" + t.Name)
			n.rate, n.name = iv, disp
		} else {
			addUnit("u_"+t.Name, t.Name, "task", t.Body, t.Region)
		}
	}
	for _, m := range prog.Machines {
		id := "u_" + m.Name
		node(id, m.Name, "machine")
		units = append(units, Unit{id, m.Name, m.Region.StartLine})
		for _, s := range m.States {
			walk(s.EnterBody, id)
			walk(s.ExitBody, id)
			walk(s.Body, id)
			for _, tr := range s.Transitions {
				walk(tr.Event, id)
				walk(tr.Guard, id)
				walk(tr.Action, id)
			}
			for _, tr := range s.TimedTransitions {
				walk(tr.Guard, id)
				walk(tr.Action, id)
			}
		}
	}
	for _, f := range prog.Functions {
		addUnit("u_"+f.Name, f.Name+"()", "func", f.Body, f.Region)
	}

	// ---- per-node metadata for the click menu ----
	svcMethods := map[string][]string{}
	for _, k := range edges.keys {
		e := edges.vals[k]
		for _, id := range []string{e.src, e.dst} {
			if strings.HasPrefix(id, "s_") {
				svcMethods[id] = append(svcMethods[id], e.label)
			}
		}
	}
	lineOf := func(r compiler.Region) *int { return &r.StartLine }
	nodeMeta := func(id string, m *gnode) *NodeInfo {
		name := id[2:]
		info := &NodeInfo{Kind: m.kind, Label: m.label}
		switch {
		case strings.HasPrefix(id, "p_") && pinDecls[name] != nil:
			p := pinDecls[name]
			info.Line = lineOf(p.Region)
			info.Detail = strings.Trim(p.Mode+" · GPIO "+strconv.Itoa(p.GPIO), " ·")
		case strings.HasPrefix(id, "c_") && chanDecls[name] != nil:
			c := chanDecls[name]
			info.Line = lineOf(c.Region)
			info.Detail = c.ElemType + "[" + strconv.Itoa(c.Capacity) + "]"
		case strings.HasPrefix(id, "v_"):
			if v := varDecls[name]; v != nil {
				info.Line = lineOf(v.Region)
				if v.Shared {
					info.Detail = "shared "
				}
				if v.TypeName != nil {
					info.Detail += *v.TypeName
				}
			} else if sd := storageDecls[name]; sd != nil {
				info.Line = lineOf(sd.Region)
				info.Detail = strings.Trim(sd.TypeName+" · storage", " ·")
			}
		case strings.HasPrefix(id, "s_"):
			ms := uniqSorted(svcMethods[id])
			info.Detail = strings.Join(ms[:min(4, len(ms))], ", ")
			if len(ms) > 4 {
				info.Detail += " …"
			}
		case strings.HasPrefix(id, "f_") && funcDecls[name] != nil:
			info.Line = lineOf(funcDecls[name].Region)
			info.Detail = "function"
		case id == "u_setup":
			if prog.Setup != nil {
				info.Line = lineOf(prog.Setup.Region)
			}
			info.Detail = "setup block"
		case strings.HasPrefix(id, "u_"):
			switch {
			case m.kind == "machine" && machineDecls[name] != nil:
				mc := machineDecls[name]
				info.Line = lineOf(mc.Region)
				info.Detail = strconv.Itoa(len(mc.States)) + " state"
				if len(mc.States) != 1 {
					info.Detail += "s"
				}
			case m.kind == "func" && funcDecls[name] != nil:
				info.Line = lineOf(funcDecls[name].Region)
				info.Detail = "function"
			case taskDecls[name] != nil:
				info.Line = lineOf(taskDecls[name].Region)
				info.Detail = "task"
				if m.kind == "timer" {
					info.Detail = "every " + m.rate
				}
			}
		}
		return info
	}
	nodeInfo := &ordered[*NodeInfo]{}
	for _, id := range nodes.keys {
		nodeInfo.set(id, nodeMeta(id, nodes.vals[id]))
	}

	// ---- assemble the Mermaid flowchart ----
	decl := func(id string) string {
		sh, ok := shapes[nodes.vals[id].kind]
		if !ok {
			sh = [2]string{`["`, `"]`}
		}
		return id + sh[0] + nodes.vals[id].label + sh[1]
	}

	// Which units touch each resource → sole owner vs shared.
	resUnits := map[string]set{}
	addRes := func(res, unit string) {
		if resUnits[res] == nil {
			resUnits[res] = set{}
		}
		resUnits[res][unit] = true
	}
	for _, k := range edges.keys {
		e := edges.vals[k]
		su, du := strings.HasPrefix(e.src, "u_"), strings.HasPrefix(e.dst, "u_")
		if su && !du {
			addRes(e.dst, e.src)
		} else if du && !su {
			addRes(e.src, e.dst)
		}
	}
	owner := map[string]string{}
	shared := set{}
	for res, us := range resUnits {
		if len(us) == 1 {
			for u := range us {
				owner[res] = u
			}
		}
		if n, ok := nodes.get(res); len(us) >= 2 && ok && (n.kind == "var" || n.kind == "channel" || n.kind == "pin") {
			shared[res] = true
		}
	}

	resKinds := set{"var": true, "channel": true, "pin": true, "service": true, "func": true}
	var loops []string
	for _, id := range nodes.keys {
		if nodes.vals[id].kind == "timer" {
			loops = append(loops, id)
		}
	}
	exp := set{}
	for _, e := range expanded {
		exp[e] = true
	}
	// A resource is hidden only when its sole owner is a collapsed loop.
	folded := func(id string) bool {
		o, ok := owner[id]
		return ok && slices.Contains(loops, o) && !exp[o]
	}

	out := []string{"flowchart LR"}
	for _, id := range nodes.keys {
		n := nodes.vals[id]
		if slices.Contains(loops, id) || resKinds[n.kind] && folded(id) {
			continue
		}
		out = append(out, "    "+decl(id))
	}
	for _, id := range loops {
		out = append(out, "    "+decl(id))
	}

	// Aggregate parallel edges; drop any touching a folded node.
	var agg ordered[[]string]
	for _, k := range edges.keys {
		e := edges.vals[k]
		if folded(e.src) || folded(e.dst) {
			continue
		}
		key := e.src + "\x00" + e.dst + "\x00" + e.kind
		labels, _ := agg.get(key)
		agg.set(key, append(labels, e.label))
	}

	anims := []EdgeAnim{}
	for _, key := range agg.keys {
		p := strings.Split(key, "\x00")
		src, dst, kind := p[0], p[1], p[2]
		arrow := "-->"
		if kind == "read" || kind == "consume" || kind == "in" {
			arrow = "-.->"
		}
		labs := uniqSorted(agg.vals[key])
		lbl := strings.Join(labs[:min(3, len(labs))], ", ")
		if len(labs) > 3 {
			lbl += ", +" + strconv.Itoa(len(labs)-3)
		}
		out = append(out, "    "+src+" "+arrow+"|"+lbl+"| "+dst)
		u := src
		if !strings.HasPrefix(src, "u_") {
			u = dst
		}
		drive := "task"
		if u == "u_setup" {
			drive = "setup"
		} else if n, ok := nodes.get(u); ok {
			drive = n.kind
		}
		a := EdgeAnim{Drive: drive, Src: src, Dst: dst}
		if drive == "timer" {
			a.hasRate = true
			n, _ := nodes.get(u)
			a.RateMS = rateToMS(n.rate)
		}
		anims = append(anims, a)
	}

	out = append(out, classDefs...)
	for _, id := range nodes.keys {
		n := nodes.vals[id]
		if resKinds[n.kind] && folded(id) {
			continue // folded → not rendered, so no class
		}
		cls := n.kind
		if shared[id] {
			cls = "shared"
		}
		out = append(out, "    class "+id+" "+cls+";")
	}

	return SystemGraphResult{
		Mermaid: strings.Join(out, "\n"), Units: units, Loops: nonNil(loops),
		Edges: anims, Nodes: nodeInfo,
	}
}

func walkAssign(target, value compiler.Node, unit string, globals set,
	node func(id, label, kind string), edge func(src, dst, kind, label string),
	walk func(any, string)) {
	for {
		d, ok := target.(*compiler.DotAccess)
		if !ok || d == nil {
			break
		}
		target = d.Obj
	}
	if v, ok := target.(*compiler.VarRef); ok && v != nil && globals[v.Name] {
		node("v_"+v.Name, v.Name, "var")
		edge(unit, "v_"+v.Name, "write", "write")
	}
	walk(value, unit)
}

type set = map[string]bool

func uniqSorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return slices.Compact(out)
}

// pyWS is Python's Unicode \s.
const pyWS = `[\t\n\v\f\r \x1c-\x1f\x85\xa0\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

var rateRe = regexp.MustCompile(`([0-9]*\.?[0-9]+)` + pyWS + `*(ms|us|s|m)?`)

var rateUnitMS = map[string]float64{"us": 0.001, "ms": 1, "s": 1000, "m": 60000}

// rateToMS parses a timer interval snippet ("100ms", "1 s") to ms.
func rateToMS(txt string) *float64 {
	m := rateRe.FindStringSubmatch(txt)
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	unit := m[2]
	if unit == "" {
		unit = "ms"
	}
	v *= rateUnitMS[unit]
	return &v
}
