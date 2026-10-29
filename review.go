package compiler

import (
	"fmt"
	"slices"
	"strings"
)

// ReviewFacts is what a program does, read off its checked AST, for
// comparing with what was asked: pins, timing, persistence, states, network.
type ReviewFacts struct {
	Chip     string         `json:"chip"`
	Pins     []PinFact      `json:"pins"`
	Devices  []DeviceFact   `json:"devices"`
	Periodic []PeriodicFact `json:"periodic"`
	Events   []EventFact    `json:"events,omitempty"`
	Tasks    []string       `json:"tasks,omitempty"`
	Storage  []StorageFact  `json:"storage,omitempty"`
	Machines []MachineFact  `json:"machines,omitempty"`
	Channels []ChannelFact  `json:"channels,omitempty"`
	Network  []string       `json:"network,omitempty"`
}

type PinFact struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	GPIO int    `json:"gpio"`
}

type DeviceFact struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Wiring  string `json:"wiring,omitempty"`
	Bus     string `json:"bus,omitempty"`
	Address string `json:"address,omitempty"`
}

type PeriodicFact struct {
	Where  string `json:"where"`
	Every  string `json:"every"`
	When   string `json:"when,omitempty"`
	Strict bool   `json:"strict,omitempty"`
}

type EventFact struct {
	Where    string `json:"where"`
	On       string `json:"on"`
	Debounce string `json:"debounce,omitempty"`
}

type StorageFact struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
	Saved   bool   `json:"saved"`
}

type MachineFact struct {
	Name    string      `json:"name"`
	Initial string      `json:"initial"`
	States  []StateFact `json:"states"`
}

type StateFact struct {
	Name        string   `json:"name"`
	Transitions []string `json:"transitions,omitempty"`
	Unreachable bool     `json:"unreachable,omitempty"`
}

type ChannelFact struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Capacity int    `json:"capacity"`
}

// networkObjects are the built-ins whose calls Review lists.
var networkObjects = []string{"wifi", "mqtt", "http", "ble"}

// Review checks source like Compile and, when it has no errors, returns its
// facts. Diagnostics come back either way.
func Review(source, filename string) (*ReviewFacts, *Errors, error) {
	prog, errs, err := compileFront(source, filename, nil)
	if err != nil || prog == nil {
		return nil, errs, err
	}
	f := &ReviewFacts{Pins: []PinFact{}, Devices: []DeviceFact{}, Periodic: []PeriodicFact{}}
	if prog.Chip != nil {
		f.Chip = prog.Chip.Target
	}
	if hw := prog.Hardware; hw != nil {
		for _, p := range hw.Pins {
			f.Pins = append(f.Pins, PinFact{p.Name, p.Mode, p.GPIO})
		}
		for _, s := range hw.Sensors {
			d := DeviceFact{Name: s.Name, Type: s.SensorType, Bus: s.BusName}
			var args []string
			for _, k := range s.PinParams.Keys {
				v := s.PinParams.Vals[k]
				if n, ok := v.(Node); ok {
					v = ExprText(n)
				}
				args = append(args, fmt.Sprintf("%s: %v", k, v))
			}
			d.Wiring = strings.Join(args, ", ")
			if s.Address != 0 {
				d.Address = fmt.Sprintf("0x%02X", s.Address)
			}
			f.Devices = append(f.Devices, d)
		}
	}

	blocks := func(where string, body []Node) {
		eachStmt(body, func(s Node) {
			switch s := s.(type) {
			case *EveryBlock:
				p := PeriodicFact{Where: where, Every: ExprText(s.Interval), Strict: s.Strict}
				if s.Guard != nil {
					p.When = ExprText(s.Guard)
				}
				f.Periodic = append(f.Periodic, p)
			case *OnBlock:
				e := EventFact{Where: where, On: ExprText(s.Event)}
				if s.Debounce != nil {
					e.Debounce = ExprText(s.Debounce)
				}
				f.Events = append(f.Events, e)
			}
		})
	}
	for _, t := range prog.Tasks {
		where := "top level"
		if t.Annotations != nil {
			where = "task " + t.Name
			f.Tasks = append(f.Tasks, t.Name)
		}
		blocks(where, t.Body)
	}

	saved := map[string]bool{}
	network := map[string]bool{}
	for _, body := range programBodies(prog) {
		eachStmt(body, func(s Node) {
			for _, e := range stmtExprs(s) {
				eachExpr(e, func(e Node) {
					mc, ok := e.(*MethodCall)
					if !ok {
						return
					}
					v, ok := mc.Obj.(*VarRef)
					if !ok {
						return
					}
					var key string
					if len(mc.Args) > 0 {
						if lit, ok := mc.Args[0].(*StringLiteral); ok {
							key = lit.Value
						}
					}
					switch {
					case v.Name == "storage" && mc.Method == "save":
						saved[key] = true
					case slices.Contains(networkObjects, v.Name):
						call := v.Name + "." + mc.Method
						if key != "" && v.Name != "wifi" { // keep credentials out
							call += fmt.Sprintf("(%q)", key)
						}
						network[call] = true
					}
				})
			}
		})
	}
	for _, sv := range prog.StorageVars {
		def := ""
		if sv.DefaultValue != nil {
			def = ExprText(sv.DefaultValue)
		}
		f.Storage = append(f.Storage, StorageFact{sv.Name, sv.TypeName, def, saved[sv.Name]})
	}
	for k := range network {
		f.Network = append(f.Network, k)
	}
	slices.Sort(f.Network)

	unreachable := unreachableStates(prog)
	for _, m := range prog.Machines {
		mf := MachineFact{Name: m.Name}
		for i, st := range m.States {
			if st.IsInitial || (i == 0 && mf.Initial == "") {
				mf.Initial = st.Name
			}
			sf := StateFact{Name: st.Name, Unreachable: unreachable[st]}
			for _, tr := range st.Transitions {
				sf.Transitions = append(sf.Transitions, "on "+ExprText(tr.Event)+guardText(tr.Guard)+" -> "+tr.TargetState)
			}
			for _, tr := range st.TimedTransitions {
				sf.Transitions = append(sf.Transitions, "after "+ExprText(tr.Duration)+guardText(tr.Guard)+" -> "+tr.TargetState)
			}
			eachStmt(slices.Concat(st.EnterBody, st.Body), func(s Node) {
				if b, ok := s.(*BecomeStmt); ok {
					sf.Transitions = append(sf.Transitions, "become "+b.Target)
				}
			})
			mf.States = append(mf.States, sf)
		}
		blocksIn := func(st *StateDecl) { blocks("machine "+m.Name+" state "+st.Name, st.Body) }
		for _, st := range m.States {
			blocksIn(st)
		}
		f.Machines = append(f.Machines, mf)
	}
	for _, c := range prog.Channels {
		f.Channels = append(f.Channels, ChannelFact{c.Name, c.ElemType, c.Capacity})
	}
	return f, errs, nil
}

func guardText(g Node) string {
	if g == nil {
		return ""
	}
	return " [" + ExprText(g) + "]"
}
