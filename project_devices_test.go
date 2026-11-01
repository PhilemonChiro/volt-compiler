package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLib writes a minimal device library folder.
func writeLib(t *testing.T, root, prefix, name string) {
	t.Helper()
	dir := filepath.Join(root, prefix)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dj := `{"name": "` + name + `", "category": "sensor", "interface": "gpio", "c_prefix": "` + prefix + `",
	  "init_args": "{pin}", "methods": {"read": {"returns": "int", "params": []}}}`
	if err := os.WriteFile(filepath.Join(dir, "device.json"), []byte(dj), 0o644); err != nil {
		t.Fatal(err)
	}
}

func withDirs(t *testing.T, shared, project string) {
	t.Helper()
	oldD, oldP := DevicesDir, ProjectDevicesDir
	DevicesDir, ProjectDevicesDir = shared, project
	registryMu.Lock()
	registry = nil
	registryMu.Unlock()
	t.Cleanup(func() {
		DevicesDir, ProjectDevicesDir = oldD, oldP
		registryMu.Lock()
		registry = nil
		registryMu.Unlock()
	})
}

func TestProjectDevicesAdded(t *testing.T) {
	shared, project := t.TempDir(), t.TempDir()
	writeLib(t, shared, "shared_dev", "SharedDev")
	writeLib(t, project, "gate_sms", "GateSms")
	withDirs(t, shared, project)
	r := Registry()
	if r.Get("SharedDev") == nil || r.Get("GateSms") == nil {
		t.Fatalf("want both libraries, got %d", len(r.Devices))
	}
	if !r.Get("GateSms").Project || r.Get("SharedDev").Project {
		t.Fatal("only the project library is marked Project")
	}
}

func TestProjectCannotReplaceShared(t *testing.T) {
	shared, project := t.TempDir(), t.TempDir()
	writeLib(t, shared, "shared_dev", "SharedDev")
	writeLib(t, project, "shared_dev", "Imposter") // same C prefix
	writeLib(t, project, "other_dev", "SharedDev") // same name
	withDirs(t, shared, project)
	r := Registry()
	if r.Get("Imposter") != nil || r.Get("SharedDev").Project {
		t.Fatal("a project library must not replace a shared one")
	}
	if len(ProjectConflicts) != 2 {
		t.Fatalf("want 2 conflicts, got %v", ProjectConflicts)
	}
	_, errs, _ := Compile("hardware {\n    device x: Imposter(pin: 4)\n}\n", "main.volt")
	var all []string
	for _, d := range errs.List {
		all = append(all, d.Message+" "+d.Hint)
	}
	if !strings.Contains(strings.Join(all, "\n"), "can't replace") {
		t.Fatalf("want a clear conflict hint, got %q", all)
	}
}
