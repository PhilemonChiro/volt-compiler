// Package harness is what lets a general-purpose model write correct Volt:
// the language reference, device cards and compiler feedback, with the
// guidance that tells a model how to use them. Front ends (the MCP server,
// `volt ai`) share it so they teach models the same workflow.
package harness

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/PhilemonChiro/volt-compiler"
	"github.com/PhilemonChiro/volt-compiler/ide"
)

// Instructions is the workflow a model must follow, for clients that put
// server instructions in the model's context.
const Instructions = `Volt is a new language for microcontroller firmware. You have not seen it in training, so never write it from memory or by analogy with C, Arduino or Python.

To write or change a Volt program:
1. Read the language reference (volt_spec, or the volt://spec resource) once per conversation.
2. Call device_card for every device the program uses; use only the methods, arguments and wiring it lists. list_devices finds devices by name or purpose.
3. Write the program, then call check on it.
4. Fix every error check reports (each has a line and usually a hint) and call check again. Repeat until check returns ok: true.
5. Call review and compare its facts with the user's request: every pin number, threshold, interval and timing they gave, button behaviour (one action per press, debouncing), values that must survive power loss, and every state the request describes. If anything differs, fix the program and go back to step 3.

Never present a Volt program that has not passed check. The chip is set in volt.toml, so never write a chip line.`

// WorkflowPrompt is the prompt template for a single program request.
func WorkflowPrompt(request string) string {
	return "Write a Volt program for this request:\n\n" + request + "\n\n" + Instructions
}

// Spec is the language reference in Markdown.
func Spec() string { return compiler.LanguageSpec() }

// specSkipped is left out of SpecSection's default answer: device_card serves
// the same facts one device at a time, and it is half the reference.
const specSkipped = "Reference: devices"

// SpecSection is the reference in pieces, so a model doesn't pay for parts it
// doesn't need. With no section it is everything but the device reference,
// followed by the list of sections; otherwise just the named "## " section
// (matched case-insensitively, by prefix or contained words).
func SpecSection(section string) string {
	full := Spec()
	parts := strings.Split(full, "\n## ")
	var titles []string
	for _, p := range parts[1:] {
		titles = append(titles, strings.SplitN(p, "\n", 2)[0])
	}
	if q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(section), "##"))); q != "" {
		for i, t := range titles {
			if lt := strings.ToLower(t); strings.HasPrefix(lt, q) || strings.Contains(lt, q) {
				return "## " + parts[i+1]
			}
		}
		return fmt.Sprintf("No section %q. Sections: %s.", section, strings.Join(titles, "; "))
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for i, p := range parts[1:] {
		if titles[i] == specSkipped {
			continue
		}
		b.WriteString("\n## " + p)
	}
	b.WriteString("\n\n---\nLeft out above: \"" + specSkipped + "\" — call device_card for each device you use (list_devices finds them). " +
		"Any one section again: volt_spec with section set to its title. Sections: " + strings.Join(titles, "; ") + ".\n")
	return b.String()
}

// Device is a registry device in brief.
type Device struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Interface   string `json:"interface"`
	Description string `json:"description"`
}

// Devices lists the registry devices whose name, category, interface or
// description contains every word of query (all devices when it is empty).
func Devices(query string) []Device {
	words := strings.Fields(strings.ToLower(query))
	out := []Device{}
	for _, d := range compiler.Registry().Devices {
		text := strings.ToLower(d.Name + " " + d.Category + " " + d.Interface + " " + d.Description)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			out = append(out, Device{d.Name, d.Category, d.Interface, d.Description})
		}
	}
	return out
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func norm(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// Cards returns the reference card of each named device. Unknown names come
// back in unknown, each with the registry names that resemble it.
func Cards(names []string) (cards []string, unknown map[string][]string) {
	unknown = map[string][]string{}
	for _, name := range names {
		if card := compiler.DeviceCard(name); card != "" {
			if ex := compiler.DeviceExample(name); ex != "" {
				card += "\n\nExample program (passes check):\n```volt\n" + strings.TrimSpace(ex) + "\n```"
			}
			cards = append(cards, card)
			continue
		}
		n := norm(name)
		similar := []string{}
		for _, d := range compiler.DeviceNames() {
			if m := norm(d); n != "" && (strings.Contains(m, n) || strings.Contains(n, m)) {
				similar = append(similar, d)
			}
		}
		unknown[name] = similar
	}
	return cards, unknown
}

// CheckResult is the compiler's verdict with the model's next step.
type CheckResult struct {
	OK          bool             `json:"ok"`
	Errors      int              `json:"errors"`
	Warnings    int              `json:"warnings"`
	Diagnostics []ide.Diagnostic `json:"diagnostics"`
	Next        string           `json:"next"`
}

// maxDiagnostics bounds a check result; the first errors matter most and
// later ones are often consequences of them.
const maxDiagnostics = 25

// Check compiles source as filename (volt.toml beside it sets the chip).
func Check(source, filename string) (*CheckResult, error) {
	res, err := ide.Check(source, filename)
	if err != nil {
		return nil, err
	}
	diags := res.Diagnostics
	if diags == nil {
		diags = []ide.Diagnostic{}
	}
	r := &CheckResult{OK: res.OK, Errors: res.Errors, Warnings: res.Warnings, Diagnostics: diags[:min(len(diags), maxDiagnostics)]}
	switch {
	case !r.OK:
		r.Next = fmt.Sprintf("Fix the %d error(s), then call check again. The program is not finished until check returns ok: true.", r.Errors)
	case r.Warnings > 0:
		r.Next = fmt.Sprintf("Compiles. Resolve the %d warning(s) unless they are intended, then call review and compare its facts with the user's request.", r.Warnings)
	default:
		r.Next = "Compiles cleanly. Now call review and compare its facts with the user's request before presenting the program."
	}
	return r, nil
}

// Format returns source in canonical Volt formatting.
func Format(source string) (string, error) { return compiler.Format(source) }

// ReviewResult is what a compiling program does, with what to compare it
// against in the user's request.
type ReviewResult struct {
	OK      bool                  `json:"ok"`
	Facts   *compiler.ReviewFacts `json:"facts,omitempty"`
	Compare []string              `json:"compare,omitempty"`
	Next    string                `json:"next"`
}

// Review lists the facts of source (filename as for Check).
func Review(source, filename string) (*ReviewResult, error) {
	facts, errs, err := compiler.Review(source, filename)
	if err != nil {
		return nil, err
	}
	if facts == nil || errs.HasErrors() {
		return &ReviewResult{Next: "The program has errors; call check and fix them first."}, nil
	}
	cmp := []string{"pins and devices: every GPIO, I2C address and wiring the user gave"}
	if len(facts.Periodic) > 0 || len(facts.Events) > 0 {
		cmp = append(cmp, "periodic and events: each interval matches the sampling rates, durations and timeouts asked for")
	}
	for _, p := range facts.Pins {
		if strings.Contains(p.Mode, "Input") {
			cmp = append(cmp, "buttons and switches: one action per press, not repeated while held; debounced if asked; active level matches the wiring")
			break
		}
	}
	cmp = append(cmp, "persistence: anything the user wants kept across power loss is a storage variable with saved: true")
	if len(facts.Machines) > 0 {
		cmp = append(cmp, "machines: every state or mode the user described exists, none is unreachable, transitions fire on the conditions asked for")
	}
	if len(facts.Network) > 0 {
		cmp = append(cmp, "network: topics, endpoints and when they are used match the request")
	}
	return &ReviewResult{OK: true, Facts: facts, Compare: cmp,
		Next: "Compare each fact with the user's request. If anything differs, fix the program, check it, and review again; otherwise present it."}, nil
}

// AgentRules is the project guidance for coding agents (AGENTS.md,
// CLAUDE.md, Cursor rules): use the volt tools, follow the workflow, and the
// spec's rules that are easiest to get wrong.
func AgentRules() string {
	var b strings.Builder
	b.WriteString(`# Volt firmware

This project's firmware is written in Volt (` + "`*.volt`" + `), a new language for microcontrollers. You have not seen it in training: never write it from memory or by analogy with C, Arduino or Python.

## Tools
The ` + "`volt`" + ` MCP server (configured in .mcp.json, .cursor/mcp.json and .vscode/mcp.json; needs ` + "`volt`" + ` on PATH) provides
volt_spec, list_devices, device_card, check, review, format and build. Without it, use the CLI:
` + "`volt spec`" + ` prints the reference, ` + "`volt check main.volt --json`" + ` reports errors, ` + "`volt build`" + ` builds.

## Workflow
1. Read the language reference once (volt_spec).
2. Read the device card of every device you use (device_card); use only what it lists.
3. Write the program, then check it. Fix every error and check again until it passes.
4. Call review and compare its facts (pins, intervals, button events, saved storage, states, network) with what was asked.
5. Build. Never present or commit Volt that has not passed check.

`)
	spec := Spec()
	if i := strings.Index(spec, "## Rules that are easy to get wrong"); i >= 0 {
		rest := spec[i:]
		if j := strings.Index(rest[3:], "\n## "); j >= 0 {
			rest = rest[:j+3]
		}
		b.WriteString(strings.TrimSpace(rest) + "\n")
	}
	return b.String()
}
