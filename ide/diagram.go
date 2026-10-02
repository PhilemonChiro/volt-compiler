package ide

import (
	"strings"

	"github.com/PhilemonChiro/volt-compiler"
	"github.com/PhilemonChiro/volt-compiler/internal/pystr"
)

type StateLine struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

type MachineDiagram struct {
	Name    string      `json:"name"`
	Mermaid string      `json:"mermaid"`
	States  []StateLine `json:"states"`
}

type StateDiagramResult struct {
	Machines []MachineDiagram `json:"machines"`
}

// StateDiagram emits a Mermaid stateDiagram-v2 per machine plus a
// state→line map. Labels are sliced from the source by region.
func StateDiagram(source string) StateDiagramResult {
	res := StateDiagramResult{Machines: []MachineDiagram{}}
	prog, err := compiler.Parse(source)
	if err != nil {
		return res
	}
	src := newSnipper(source)
	text := func(n compiler.Node) string { return label(src.snip(widestRegion(n))) }

	for _, m := range prog.Machines {
		header := []string{"stateDiagram-v2"}
		var initial *compiler.StateDecl
		for _, s := range m.States {
			if s.IsInitial {
				initial = s
				break
			}
		}
		if initial == nil && len(m.States) > 0 {
			initial = m.States[0]
		}
		if initial != nil {
			header = append(header, "    [*] --> "+initial.Name)
		}

		var edges []string
		states := []StateLine{}
		for _, s := range m.States {
			states = append(states, StateLine{s.Name, s.Region.StartLine})
			for _, t := range s.Transitions {
				lbl := text(t.Event)
				if guard := text(t.Guard); guard != "" {
					if lbl != "" {
						lbl += " [" + guard + "]"
					} else {
						lbl = "[" + guard + "]"
					}
				}
				edge := "    " + s.Name + " --> " + t.TargetState
				if lbl != "" {
					edge += " : " + lbl
				}
				edges = append(edges, edge)
			}
			for _, t := range s.TimedTransitions {
				lbl := "after"
				if dur := text(t.Duration); dur != "" {
					lbl += " " + dur
				}
				if guard := text(t.Guard); guard != "" {
					lbl += " [" + guard + "]"
				}
				edges = append(edges, "    "+s.Name+" --> "+t.TargetState+" : "+lbl)
			}

			// Imperative `become <state>` from enter/exit/body and from any
			// transition's action block.
			var becomes []string
			collectBecomes(s.EnterBody, &becomes)
			collectBecomes(s.ExitBody, &becomes)
			collectBecomes(s.Body, &becomes)
			for _, t := range s.Transitions {
				collectBecomes(t.Action, &becomes)
			}
			for _, t := range s.TimedTransitions {
				collectBecomes(t.Action, &becomes)
			}
			seen := map[string]bool{}
			for _, target := range becomes {
				if !seen[target] {
					seen[target] = true
					edges = append(edges, "    "+s.Name+" --> "+target+" : become")
				}
			}
		}
		res.Machines = append(res.Machines, MachineDiagram{
			m.Name, strings.Join(append(header, edges...), "\n"), states})
	}
	return res
}

// label makes an edge label single-line and free of Mermaid's ':'.
func label(s string) string {
	return pystr.Strip(strings.ReplaceAll(strings.Join(pystr.Fields(s), " "), ":", " "))
}

// widestRegion spans every non-empty region in the subtree (a BinaryExpr's
// own region often covers only its left operand).
func widestRegion(n any) *compiler.Region {
	var acc []compiler.Region
	var walk func(any)
	walk = func(n any) {
		if isNil(n) {
			return
		}
		if items, ok := list(n); ok {
			for _, x := range items {
				walk(x)
			}
			return
		}
		if r := regionOf(n); r != nil {
			if r.EndLine != 0 || r.EndCol != 0 {
				acc = append(acc, *r)
			}
			children(n, walk)
		}
	}
	walk(n)
	if len(acc) == 0 {
		return nil
	}
	w := acc[0]
	for _, r := range acc[1:] {
		if r.StartLine < w.StartLine || r.StartLine == w.StartLine && r.StartCol < w.StartCol {
			w.StartLine, w.StartCol = r.StartLine, r.StartCol
		}
		if r.EndLine > w.EndLine || r.EndLine == w.EndLine && r.EndCol > w.EndCol {
			w.EndLine, w.EndCol = r.EndLine, r.EndCol
		}
	}
	return &w
}

func collectBecomes(n any, out *[]string) {
	if isNil(n) {
		return
	}
	if items, ok := list(n); ok {
		for _, x := range items {
			collectBecomes(x, out)
		}
		return
	}
	if b, ok := n.(*compiler.BecomeStmt); ok {
		if b.Target != "" {
			*out = append(*out, b.Target)
		}
		return
	}
	children(n, func(v any) { collectBecomes(v, out) })
}
