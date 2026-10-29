package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	DefaultAddress any      // string or number, as written in the JSON
	Addresses      []string // every selectable I2C address, default first ("0x76", "0x77")
	Description    string
	CPrefix        string
	CSource        string
	InitArgs       string // placeholders: {pin}, {addr}, {bus_id}, {uart_id}, pin roles, params
	// Pins are the device's own GPIO connections (not bus signals), in
	// declaration order: `HCSR04(trig: 26, echo: 27)`.
	Pins []DevicePin
	// Params are non-pin settings with defaults: `NeoPixel(pin: 32, leds: 30)`.
	Params []DeviceParam
	// MaxSpeed is the fastest bus clock (Hz) the part supports; 0 = no limit.
	MaxSpeed int
	Methods  []*DeviceMethod
}

type DevicePin struct {
	Name string
	Dir  string // "out", "in" or "io"
}

type DeviceParam struct {
	Name    string
	Type    string // "int", "float", "bool" or "string"
	Default any
}

// Pin returns the device pin named name, or nil.
func (d *DeviceInfo) Pin(name string) *DevicePin {
	for i := range d.Pins {
		if d.Pins[i].Name == name {
			return &d.Pins[i]
		}
	}
	return nil
}

// Param returns the device parameter named name, or nil.
func (d *DeviceInfo) Param(name string) *DeviceParam {
	for i := range d.Params {
		if d.Params[i].Name == name {
			return &d.Params[i]
		}
	}
	return nil
}

// DefaultAddr is the default I2C address as a number (-1 if none).
func (d *DeviceInfo) DefaultAddr() int {
	switch v := d.DefaultAddress.(type) {
	case float64:
		return int(v)
	case string:
		if n, ok := parseAddr(v); ok {
			return n
		}
	}
	return -1
}

// parseAddr reads "0x76" or "118".
func parseAddr(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	base := 10
	if strings.HasPrefix(s, "0x") {
		s, base = s[2:], 16
	}
	n, err := strconv.ParseInt(s, base, 32)
	return int(n), err == nil && s != ""
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
	registryMu      sync.Mutex
	registry        *DeviceRegistry
	registryDir     string
	registryStamp   string
	registryChecked time.Time
)

// Registry returns the device registry for the current devices dir. It
// reloads when the dir changes or a descriptor is added, removed or edited
// (checked at most once a second), so long-running processes such as
// `volt mcp` and the IDE worker never serve a stale catalog.
func Registry() *DeviceRegistry {
	registryMu.Lock()
	defer registryMu.Unlock()
	dir := devicesDir()
	if registry != nil && dir == registryDir && time.Since(registryChecked) < time.Second {
		return registry
	}
	stamp := descriptorStamp(dir)
	registryChecked = time.Now()
	if registry == nil || dir != registryDir || stamp != registryStamp {
		registry, registryDir, registryStamp = loadRegistry(dir), dir, stamp
	}
	return registry
}

// descriptorStamp summarizes every .json under dir (path, size, mtime).
func descriptorStamp(dir string) string {
	if dir == "" {
		return ""
	}
	var b strings.Builder
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", path, fi.Size(), fi.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
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
		Addresses      []string        `json:"addresses"`
		Description    string          `json:"description"`
		CPrefix        *string         `json:"c_prefix"`
		CSource        string          `json:"c_source"`
		InitArgs       string          `json:"init_args"`
		Pins           json.RawMessage `json:"pins"`
		Params         json.RawMessage `json:"params"`
		MaxSpeed       int             `json:"max_speed"`
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
		DefaultAddress: raw.DefaultAddress, Addresses: raw.Addresses, Description: raw.Description,
		CPrefix: deref(raw.CPrefix, strings.ToLower(*raw.Name)), CSource: raw.CSource, InitArgs: raw.InitArgs,
	}
	if dev.DefaultAddress == nil {
		dev.DefaultAddress = ""
	}
	dev.MaxSpeed = raw.MaxSpeed
	if len(raw.Pins) > 0 {
		names, vals, err := orderedObject(raw.Pins)
		if err != nil {
			return nil, fmt.Errorf("pins: %v", err)
		}
		for i, name := range names {
			var p struct {
				Dir string `json:"dir"`
			}
			if err := json.Unmarshal(vals[i], &p); err != nil {
				return nil, fmt.Errorf("pin %s: %v", name, err)
			}
			if p.Dir == "" {
				p.Dir = "io"
			}
			dev.Pins = append(dev.Pins, DevicePin{name, p.Dir})
		}
	}
	if len(raw.Params) > 0 {
		names, vals, err := orderedObject(raw.Params)
		if err != nil {
			return nil, fmt.Errorf("params: %v", err)
		}
		for i, name := range names {
			var p struct {
				Type    string `json:"type"`
				Default any    `json:"default"`
			}
			if err := json.Unmarshal(vals[i], &p); err != nil {
				return nil, fmt.Errorf("param %s: %v", name, err)
			}
			if p.Type == "" {
				p.Type = "int"
			}
			dev.Params = append(dev.Params, DeviceParam{name, p.Type, p.Default})
		}
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

// DeviceExample returns a complete example program for a device: the one
// shipped beside the device library (<devices>/../examples/devices/<Name>.volt),
// else the first "examples" entry in the device's own device.json. "" if none.
func DeviceExample(name string) string {
	dir := devicesDir()
	if dir == "" || name == "" {
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "examples", "devices", name+".volt")); err == nil {
		return string(b)
	}
	b, err := os.ReadFile(filepath.Join(dir, strings.ToLower(name), "device.json"))
	if err != nil {
		return ""
	}
	var d struct {
		Examples []struct {
			Code string `json:"code"`
		} `json:"examples"`
	}
	if json.Unmarshal(b, &d) == nil && len(d.Examples) > 0 {
		return d.Examples[0].Code
	}
	return ""
}
