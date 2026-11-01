// Package worker is the IDE worker (a drop-in for the original Python
// worker.py): a long-lived process answering
// JSON-line requests on stdin.
//
//	Request:  {"id": <any>, "op": "<name>", ...op-specific fields}
//	Response: {"id": <id>, "ok": true,  "result": ...}
//	          {"id": <id>, "ok": false, "error": "...", "trace": "..."}
//	Progress: {"id": <id>, "progress": {"phase": "...", "pct": N}}
//	          (compile_full with "emit_progress": true)
//	Ready:    {"id": "_ready", "ok": true} at startup
//
// Ops: compile_full, check, format_code, find_symbols, state_diagram, system_graph, sequence_diagram,
// pin_table / bus_pins ({"chip": ...}, no source).
package worker

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"

	"github.com/PhilemonChiro/volt-compiler"
	"github.com/PhilemonChiro/volt-compiler/ide"
	"github.com/PhilemonChiro/volt-compiler/internal/pystr"
)

type request struct {
	ID           json.RawMessage `json:"id"`
	Op           string          `json:"op"`
	Source       *string         `json:"source"`
	Filename     string          `json:"filename"`
	Project      *string         `json:"project"` // project folder: its devices/ adds project libraries
	Chip         string          `json:"chip"`
	Expanded     []string        `json:"expanded"`
	EmitProgress bool            `json:"emit_progress"`
}

type okResponse struct {
	ID     json.RawMessage `json:"id"`
	OK     bool            `json:"ok"`
	Result any             `json:"result"`
}

type errResponse struct {
	ID    json.RawMessage `json:"id"`
	OK    bool            `json:"ok"`
	Error string          `json:"error"`
	Trace string          `json:"trace,omitempty"`
}

type progressEvent struct {
	ID       json.RawMessage `json:"id"`
	Progress struct {
		Phase string `json:"phase"`
		Pct   int    `json:"pct"`
	} `json:"progress"`
}

// Serve runs the worker protocol: it writes the ready line, then answers
// each JSON line read from in until EOF.
func Serve(in io.Reader, out io.Writer) error {
	w := bufio.NewWriter(out)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	send := func(r any) {
		enc.Encode(r)
		w.Flush()
	}
	send(map[string]any{"id": "_ready", "ok": true})

	r := bufio.NewReaderSize(in, 1<<20)
	for {
		raw, err := r.ReadString('\n')
		if line := pystr.Strip(raw); line != "" {
			send(handle(line, send))
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func handle(line string, send func(any)) (resp any) {
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return errResponse{ID: json.RawMessage(`"_error"`), Error: "bad request: " + err.Error()}
	}
	if req.ID == nil {
		req.ID = json.RawMessage(`""`)
	}
	defer func() {
		if r := recover(); r != nil {
			resp = errResponse{ID: req.ID, Error: fmt.Sprint(r), Trace: string(debug.Stack())}
		}
	}()
	result, err := run(req, send)
	if err != nil {
		return errResponse{ID: req.ID, Error: err.Error()}
	}
	return okResponse{ID: req.ID, OK: true, Result: result}
}

var errNoSource = errors.New("missing field: source")

func run(req request, send func(any)) (any, error) {
	// Each request says which project it's for (absent = keep the last one).
	if req.Project != nil {
		compiler.ProjectDevicesDir = ""
		if *req.Project != "" {
			compiler.ProjectDevicesDir = filepath.Join(*req.Project, "devices")
		}
	}
	if req.Op == "pin_table" {
		return compiler.PinTable(req.Chip), nil
	}
	if req.Op == "bus_pins" {
		return compiler.BusPins(req.Chip), nil
	}
	switch req.Op {
	case "compile_full", "check", "format_code", "find_symbols", "state_diagram", "system_graph", "sequence_diagram":
	default:
		return nil, fmt.Errorf("unknown op: %s", req.Op)
	}
	if req.Source == nil {
		return nil, errNoSource
	}
	src := *req.Source
	switch req.Op {
	case "compile_full":
		filename := req.Filename
		if filename == "" {
			filename = "main.volt"
		}
		var cb compiler.ProgressFunc
		if req.EmitProgress {
			cb = func(phase string, pct int) {
				ev := progressEvent{ID: req.ID}
				ev.Progress.Phase, ev.Progress.Pct = phase, pct
				send(ev)
			}
		}
		return ide.CompileFull(src, filename, cb)
	case "check":
		filename := req.Filename
		if filename == "" {
			filename = "main.volt"
		}
		return ide.Check(src, filename)
	case "format_code":
		return ide.FormatCode(src)
	case "find_symbols":
		return ide.FindSymbols(src), nil
	case "state_diagram":
		return ide.StateDiagram(src), nil
	default:
		return ide.SystemGraph(src, req.Expanded), nil
	case "sequence_diagram":
		return ide.SequenceDiagram(src), nil
	}
}
