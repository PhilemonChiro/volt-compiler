package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Port of device_registry.py: device descriptors loaded from
// devices/**/*.json. Used by the checker (method types) and codegen.

// DevicesDir overrides where device descriptors are loaded from. When empty,
// $VOLT_DEVICES then ~/.volt/devices are tried. Set it before the first compile.
var DevicesDir string

// JSON type name -> checker type; anything else is kept verbatim.
var deviceTypeMap = map[string]string{
	"int": "int", "float": "float", "bool": "bool", "string": "string", "void": "void",
	"u8": "int", "u16": "int", "u32": "int", "i8": "int", "i16": "int", "i32": "int",
}

func mapDeviceType(t string) string {
	if m, ok := deviceTypeMap[t]; ok {
		return m
	}
	return t
}

// DeviceInfo is one device type.
type DeviceInfo struct {
	Name           string
	Category       string
	Interface      string
	DefaultAddress any // string or number, as written in the JSON
	Description    string
	CPrefix        string
	CSource        string
	InitArgs       string // placeholders: {pin}, {addr}, {bus_id}
	Methods        []*DeviceMethod
}

type DeviceMethod struct {
	Name        string
	Returns     string
	Params      []SymParam
	Description string
	// Blocking marks calls that can wait on I/O for seconds (modem, network).
	// A task that calls one runs as its own FreeRTOS task (priority tier), so
	// it cannot stall the background scheduler.
	Blocking bool
}

// DeviceRegistry holds every device type, in load order.
type DeviceRegistry struct {
	Devices []*DeviceInfo
	byName  map[string]*DeviceInfo
}

var (
	registryOnce sync.Once
	registry     *DeviceRegistry
)

// Registry returns the global device registry, loading it on first use.
func Registry() *DeviceRegistry {
	registryOnce.Do(func() { registry = loadRegistry(devicesDir()) })
	return registry
}

func devicesDir() string {
	if DevicesDir != "" {
		return DevicesDir
	}
	if d := os.Getenv("VOLT_DEVICES"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".volt", "devices")
	}
	return ""
}

// loadRegistry reads every .json file under dir (lexical walk order).
// A missing dir gives an empty registry; malformed files are skipped.
func loadRegistry(dir string) *DeviceRegistry {
	r := &DeviceRegistry{byName: map[string]*DeviceInfo{}}
	if dir == "" {
		return r
	}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		dev, err := parseDevice(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping malformed device file %s: %v\n", path, err)
			return nil
		}
		if _, ok := r.byName[dev.Name]; !ok {
			r.Devices = append(r.Devices, dev)
		} else {
			for i, old := range r.Devices {
				if old.Name == dev.Name {
					r.Devices[i] = dev
				}
			}
		}
		r.byName[dev.Name] = dev
		return nil
	})
	return r
}

func parseDevice(path string) (*DeviceInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Name           *string         `json:"name"`
		Category       *string         `json:"category"`
		Interface      *string         `json:"interface"`
		DefaultAddress any             `json:"default_address"`
		Description    string          `json:"description"`
		CPrefix        *string         `json:"c_prefix"`
		CSource        string          `json:"c_source"`
		InitArgs       string          `json:"init_args"`
		Methods        json.RawMessage `json:"methods"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.Name == nil {
		return nil, fmt.Errorf("missing \"name\"")
	}
	dev := &DeviceInfo{
		Name: *raw.Name, Category: deref(raw.Category, "sensor"), Interface: deref(raw.Interface, "i2c"),
		DefaultAddress: raw.DefaultAddress, Description: raw.Description,
		CPrefix: deref(raw.CPrefix, strings.ToLower(*raw.Name)), CSource: raw.CSource, InitArgs: raw.InitArgs,
	}
	if dev.DefaultAddress == nil {
		dev.DefaultAddress = ""
	}
	if len(raw.Methods) == 0 {
		return dev, nil
	}
	names, vals, err := orderedObject(raw.Methods)
	if err != nil {
		return nil, err
	}
	for i, name := range names {
		var m struct {
			Returns *string `json:"returns"`
			Params  []struct {
				Name *string `json:"name"`
				Type *string `json:"type"`
			} `json:"params"`
			Description string `json:"description"`
			Blocking    bool   `json:"blocking"`
		}
		if err := json.Unmarshal(vals[i], &m); err != nil {
			return nil, err
		}
		if m.Returns == nil {
			return nil, fmt.Errorf("method %s: missing \"returns\"", name)
		}
		dm := &DeviceMethod{Name: name, Returns: mapDeviceType(*m.Returns), Description: m.Description, Blocking: m.Blocking}
		for _, p := range m.Params {
			if p.Name == nil || p.Type == nil {
				return nil, fmt.Errorf("method %s: param missing name/type", name)
			}
			dm.Params = append(dm.Params, SymParam{*p.Name, mapDeviceType(*p.Type)})
		}
		dev.Methods = append(dev.Methods, dm)
	}
	return dev, nil
}

// orderedObject splits a JSON object into its keys (in source order) and raw
// values. A repeated key keeps its first position and last value, like Python.
func orderedObject(b []byte) ([]string, []json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, nil, fmt.Errorf("expected object")
	}
	var keys []string
	var vals []json.RawMessage
	index := map[string]int{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		k := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		if i, ok := index[k]; ok {
			vals[i] = v
			continue
		}
		index[k] = len(keys)
		keys, vals = append(keys, k), append(vals, v)
	}
	return keys, vals, nil
}

func deref(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// Get returns the device named name, or nil.
func (r *DeviceRegistry) Get(name string) *DeviceInfo {
	return r.byName[name]
}
