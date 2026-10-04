package ide

import (
	"os"
	"path/filepath"
	"testing"
)

// The whole-program sequence diagram, as Mermaid source.
// Regenerate with: go test ./ide -run SequenceDiagram -update
func TestSequenceDiagram(t *testing.T) {
	dir := filepath.Join("..", "testdata", "golden", "sequence", "plant")
	src, err := os.ReadFile(filepath.Join(dir, "main.volt"))
	if err != nil {
		t.Fatal(err)
	}
	got := SequenceDiagram(string(src)).Mermaid + "\n"
	path := filepath.Join(dir, "sequence.mmd")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, _ := os.ReadFile(path)
	if got != string(want) {
		t.Errorf("sequence differs from %s", path)
	}
	if r := SequenceDiagram("hardware {"); r.Mermaid != "" {
		t.Errorf("unparsable source gave a diagram")
	}
}
