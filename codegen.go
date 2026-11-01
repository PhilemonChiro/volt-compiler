package compiler

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Port of codegen.py + codegen_utils.py (Pass 7: transformed AST -> C).
// Output must match the Python compiler byte for byte, including its
// quirks; comments mark where Python behaviour is being reproduced.

// codegenPanic stands in for an exception the Python code generator would
// raise; Compile turns it into an error.
type codegenPanic struct{ msg string }

// lookupMethod is METHOD_MAP after the device registry is merged in: static
// entries win. ok with a nil template is Python's None entry.
func lookupMethod(typeName, method string) (tmpl *string, ok bool) {
	if t, ok := staticMethodMap[[2]string{typeName, method}]; ok {
		return t, true
	}
	dev := Registry().Get(typeName)
	if dev == nil {
		return nil, false
	}
	var found *DeviceMethod
	for _, m := range dev.Methods {
		if m.Name == method {
			found = m // last wins, like a Python dict built from JSON
		}
	}
	if found == nil {
		return nil, false
	}
	if len(found.Params) > 0 {
		return ptr(dev.CPrefix + "_" + method + "({sensor}, {args})"), true
	}
	return ptr(dev.CPrefix + "_" + method + "({sensor})"), true
}

var formatFieldRe = regexp.MustCompile(`\{\{|\}\}|\{([^{}]*)\}`)

// pyFormat is str.format with keyword arguments only.
func pyFormat(tmpl *string, kw map[string]string) string {
	if tmpl == nil {
		panic(codegenPanic{"AttributeError: 'NoneType' object has no attribute 'format'"})
	}
	return formatFieldRe.ReplaceAllStringFunc(*tmpl, func(m string) string {
		switch m {
		case "{{":
			return "{"
		case "}}":
			return "}"
		}
		key := m[1 : len(m)-1]
		v, ok := kw[key]
		if !ok {
			panic(codegenPanic{fmt.Sprintf("KeyError: '%s'", key)})
		}
		return v
	})
}

func cName(name string) string {
	if cKeywords[name] {
		return "v_" + name
	}
	return name
}

func cType(typeName string, def string) string {
	if t, ok := cTypeMap[typeName]; ok {
		return t
	}
	return def
}

func cTypeP(typeName *string, def string) string {
	if typeName == nil {
		return def
	}
	return cType(*typeName, def)
}

func msOf(d *DurationLiteral) int { return floorDiv(d.Microseconds, 1000) }

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

type codegen struct {
	sourceFile string
	lines      []string
	indent     int

	pinMap          map[string]*PinDecl // name -> pin (gpio, mode)
	sensorMap       map[string]string   // name -> sensor type
	builtinTypes    map[string]string
	uartPorts       map[string]int
	sharedVars      map[string]bool
	varTypes        map[string]string
	funcReturnTypes map[string]string
	enumNames       []string // insertion order of enumTypes
	enumTypes       map[string][]string
	locals          map[string]bool // locals/params of the body being emitted; shadow enum variants

	sprintfBufIdx      int
	chipFamily         string
	allStmts           []Node
	allStmtsDone       bool
	skipCounters       map[string]bool
	storageStrBufCount int
	tryCounter         int
	currentMachine     string
}

// Generate generates C source from a transformed Volt AST.
func Generate(program *Program, sourceFile string) (code string, err error) {
	defer func() {
		if r := recover(); r != nil {
			p, ok := r.(codegenPanic)
			if !ok {
				panic(r)
			}
			err = fmt.Errorf("codegen: %s", p.msg)
		}
	}()
	g := &codegen{
		sourceFile:      sourceFile,
		pinMap:          map[string]*PinDecl{},
		sensorMap:       map[string]string{},
		sharedVars:      map[string]bool{},
		varTypes:        map[string]string{},
		funcReturnTypes: map[string]string{},
		enumTypes:       map[string][]string{},
		locals:          map[string]bool{},
		skipCounters:    map[string]bool{},
	}
	return g.generate(program), nil
}

func (g *codegen) generate(p *Program) string {
	g.buildMaps(p)
	g.chipFamily = chipFamily(p)

	g.emitHeader(p)
	g.emitEnums(p)
	g.emitForwardDecls(p)
	g.emitGlobalVars(p)
	g.emitContextStructs(p)
	g.emitStorageVars(p)
	g.emitChannels(p)
	g.emitSensorInstances(p)
	g.emitFunctions(p)
	g.emitTasks(p)
	// Names already declared in C: a global var used as an event (`on request`)
	// or an event shared by two machines must not get a second flag.
	declared := map[string]bool{}
	for _, v := range p.GlobalVars {
		declared[v.Name] = true
	}
	for _, m := range p.Machines {
		g.emitMachine(m, declared)
	}
	g.emitAppMain(p)

	output := strings.Join(g.lines, "\n") + "\n"
	if len(g.skipCounters) > 0 {
		names := make([]string, 0, len(g.skipCounters))
		for n := range g.skipCounters {
			names = append(names, n)
		}
		sort.Strings(names)
		decls := make([]string, len(names))
		for i, n := range names {
			decls[i] = "static uint32_t " + n + " = 0;  // or-skip counter"
		}
		output = strings.ReplaceAll(output, "static char _volt_strbuf[16][128];\n",
			"static char _volt_strbuf[16][128];\n\n// Skip counters (or-skip observability)\n"+strings.Join(decls, "\n")+"\n")
	}
	if strings.Contains(output, "_volt_str_") || strings.Contains(output, "strtol(") || strings.Contains(output, "strtof(") {
		output = strings.Replace(output, "static char _volt_strbuf[16][128];\n",
			"static char _volt_strbuf[16][128];\n"+stringRuntime, 1)
	}
	if strings.Contains(output, "_volt_mqtt_") {
		output = strings.Replace(output, "static char _volt_strbuf[16][128];\n",
			"static char _volt_strbuf[16][128];\n"+mqttReceiveRuntime, 1)
	}
	return resetLineMapping(output)
}

// stringRuntime backs the string builtins (starts_with, ends_with, to_int, to_float).
const stringRuntime = `
#include <stdlib.h>
static inline bool _volt_str_starts_with(const char *s, const char *p) {
    return strncmp(s, p, strlen(p)) == 0;
}
static inline bool _volt_str_ends_with(const char *s, const char *x) {
    size_t n = strlen(s), m = strlen(x);
    return m <= n && strcmp(s + n - m, x) == 0;
}
`

// mqttReceiveRuntime keeps the latest message from any mqtt.on_receive topic
// for mqtt.has_message() / mqtt.payload() / mqtt.topic().
const mqttReceiveRuntime = `
// MQTT receive (latest message wins)
static char _volt_mqtt_topic[128];
static char _volt_mqtt_payload[512];
static char _volt_mqtt_payload_out[512];
static volatile bool _volt_mqtt_has_message = false;
static void _volt_mqtt_handler(const char *topic, const char *data, int data_len) {
    if (data_len >= (int)sizeof(_volt_mqtt_payload)) data_len = sizeof(_volt_mqtt_payload) - 1;
    if (data_len < 0) data_len = 0;
    snprintf(_volt_mqtt_topic, sizeof(_volt_mqtt_topic), "%s", topic ? topic : "");
    memcpy(_volt_mqtt_payload, data, data_len);
    _volt_mqtt_payload[data_len] = '\0';
    _volt_mqtt_has_message = true;
}
static const char *_volt_mqtt_take_payload(void) {
    memcpy(_volt_mqtt_payload_out, _volt_mqtt_payload, sizeof(_volt_mqtt_payload_out));
    _volt_mqtt_has_message = false;
    return _volt_mqtt_payload_out;
}
`

// resetLineMapping ends each function that carries #line directives with
// `#line N "main.c"`, so the scaffolding after it (app_main, ISR wrappers,
// helpers) maps to the generated file instead of continuing to count
// main.volt lines. N is the next line's own number in the output.
func resetLineMapping(code string) string {
	lines := strings.Split(code, "\n")
	out := make([]string, 0, len(lines)+8)
	mapped := false
	for _, l := range lines {
		out = append(out, l)
		switch {
		case strings.HasPrefix(l, "#line "):
			mapped = true
		case l == "}" && mapped:
			out = append(out, fmt.Sprintf(`#line %d "main.c"`, len(out)+2))
			mapped = false
		}
	}
	return strings.Join(out, "\n")
}

// ============================================================
// Map building
// ============================================================

func (g *codegen) buildMaps(p *Program) {
	if p.Hardware != nil {
		for _, pin := range p.Hardware.Pins {
			g.pinMap[pin.Name] = pin
		}
		for _, s := range p.Hardware.Sensors {
			g.sensorMap[s.Name] = s.SensorType
		}
	}
	g.builtinTypes = map[string]string{
		"wifi": "WiFi", "mqtt": "MQTT", "http": "HTTP",
		"storage": "Storage", "system": "System", "ota": "OTA",
	}
	for _, ch := range p.Channels {
		g.builtinTypes[ch.Name] = "Channel"
	}
	// Each UART/Serial bus gets its own hardware port. On ESP32 port 0 is
	// the USB console, so user buses start at 1 there; other chips at 0.
	g.uartPorts = map[string]int{}
	if p.Hardware != nil {
		base := 0
		if chipFamily(p) == "esp32" {
			base = 1
		}
		for _, bus := range p.Hardware.Buses {
			if bus.BusType == "uart" || bus.BusType == "serial" {
				g.builtinTypes[bus.Name] = "UART"
				g.uartPorts[bus.Name] = base + len(g.uartPorts)
			}
		}
	}
	for _, sv := range p.StorageVars {
		g.varTypes[sv.Name] = sv.TypeName
	}
	for _, gv := range p.GlobalVars {
		if gv.TypeName != nil && *gv.TypeName != "" {
			g.varTypes[gv.Name] = *gv.TypeName
		} else if gv.Value != nil {
			if t := g.inferValueType(gv.Value); t != "int" {
				g.varTypes[gv.Name] = t
			}
		}
	}
	for _, f := range p.Functions {
		if f.ReturnType != nil && *f.ReturnType != "" {
			g.funcReturnTypes[f.Name] = *f.ReturnType
		}
	}
}

// ============================================================
// Section emitters
// ============================================================

// chipFamily returns "esp32", "esp8266", "rp2040" or "stm32" (default esp32).
// namedPin: a device's `key: N` pin parameter (`NeoPixel(pin: 4)`), else
// its `at N` address (`NeoPixel on gpio at 4`).
func namedPin(s *SensorDecl, key string) string {
	if v, ok := s.PinParams.Vals[key]; ok {
		return pyStr(v)
	}
	return strconv.Itoa(s.Address)
}

func chipFamily(p *Program) string {
	if p.Chip != nil {
		t := strings.ToLower(p.Chip.Target)
		switch {
		case strings.Contains(t, "rp2040") || strings.Contains(t, "pico"):
			return "rp2040"
		case strings.Contains(t, "8266"):
			return "esp8266"
		case strings.Contains(t, "stm32"):
			return "stm32"
		}
	}
	return "esp32"
}

func (g *codegen) emitHeader(p *Program) {
	chip := chipFamily(p)
	g.out("// Generated by Volt compiler — do not edit")
	g.out("// Source: " + g.sourceFile)
	g.out("")
	g.out("#include <stdint.h>")
	g.out("#include <stdatomic.h>")
	if chip == "rp2040" {
		g.out(`#include "pico/stdlib.h"`)
	}
	g.out("#include <stdbool.h>")
	g.out("#include <string.h>")
	g.out("#include <stdio.h>")
	g.out(`#include "volt/volt.h"`)
	// IRAM_ATTR for critical tasks
	if (chip == "esp32" || chip == "esp8266") && usesCriticalTask(p) {
		g.out(`#include "esp_attr.h"`)
	}
	if g.usesCalls(p, "wifi") {
		g.out(`#include "volt/wifi.h"`)
	}
	if g.usesCalls(p, "mqtt") {
		g.out(`#include "volt/mqtt.h"`)
	}
	if g.usesCalls(p, "http") {
		g.out(`#include "volt/http.h"`)
	}
	if g.usesStorage(p) {
		g.out(`#include "volt/storage.h"`)
	}
	if g.usesCalls(p, "ble") {
		g.out(`#include "volt/ble.h"`)
	}
	hw := p.Hardware
	if hw != nil && len(hw.Buses) > 0 {
		busTypes := map[string]bool{}
		for _, b := range hw.Buses {
			busTypes[b.BusType] = true
		}
		if busTypes["i2c"] || busTypes["softi2c"] {
			g.out(`#include "volt/i2c.h"`)
		}
		if busTypes["spi"] {
			g.out(`#include "volt/spi.h"`)
		}
		if busTypes["uart"] {
			g.out(`#include "volt/uart.h"`)
		}
	}
	if hw != nil && slices.ContainsFunc(hw.Pins, func(p *PinDecl) bool { return p.Mode == "Servo" }) {
		g.out(`#include "volt/servo.h"`)
	}
	if hw != nil && slices.ContainsFunc(hw.Pins, func(p *PinDecl) bool { return p.Mode == "Touch" }) {
		g.out(`#include "volt/touch.h"`)
	}
	if hw != nil && len(hw.Sensors) > 0 {
		g.out(`#include "volt/sensor.h"`)
		// SPI devices (MFRC522, NRF24L01, …) live in a separate header.
		if slices.ContainsFunc(hw.Sensors, func(s *SensorDecl) bool {
			d := Registry().Get(s.SensorType)
			return d != nil && strings.EqualFold(d.Interface, "spi")
		}) {
			g.out(`#include "volt/spi_devices.h"`)
		}
		// GPIO-bound devices (DHT11, HC-SR04, …) live in a separate header.
		if slices.ContainsFunc(hw.Sensors, func(s *SensorDecl) bool {
			d := Registry().Get(s.SensorType)
			return d != nil && d.Interface == "gpio"
		}) {
			g.out(`#include "volt/gpio_devices.h"`)
		}
	}
	g.out("")
	g.out("static char _volt_strbuf[16][128];")
	if g.usesStorage(p) {
		g.out("static char _volt_storage_strbuf[8][128];")
	}
	g.out("")
}

func (g *codegen) emitEnums(p *Program) {
	for _, e := range p.Enums {
		g.out("typedef enum {")
		for i, v := range e.Variants {
			comma := ""
			if i < len(e.Variants)-1 {
				comma = ","
			}
			g.out("    " + strings.ToUpper(e.Name) + "_" + strings.ToUpper(v) + comma)
		}
		g.out("} " + strings.ToLower(e.Name) + "_t;")
		g.out("")
		if _, ok := g.enumTypes[e.Name]; !ok {
			g.enumNames = append(g.enumNames, e.Name)
		}
		g.enumTypes[e.Name] = e.Variants
	}
}

func (g *codegen) funcSignature(f *FuncDecl) string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		params[i] = g.cType(p.TypeName) + " " + cName(p.Name)
	}
	ps := strings.Join(params, ", ")
	if ps == "" {
		ps = "void"
	}
	ret := "void"
	if f.ReturnType != nil && *f.ReturnType != "" {
		ret = g.cType(*f.ReturnType)
	}
	return "static " + ret + " " + cName(f.Name) + "(" + ps + ")"
}

func (g *codegen) emitForwardDecls(p *Program) {
	g.out("// Forward declarations")
	for _, t := range p.Tasks {
		switch t.Tier {
		case "background":
			g.out("static void " + t.Name + "_run(volt_task_t *_vt);")
		case "priority":
			g.out("static void " + t.Name + "_run(void *arg);")
		case "critical":
			g.out("static void " + t.Name + "_isr(void *arg);")
		}
	}
	for _, f := range p.Functions {
		if !f.ContainsYield {
			g.out(g.funcSignature(f) + ";")
		}
	}
	for _, m := range p.Machines {
		g.out("static void " + m.Name + "_run(volt_task_t *_vt);")
	}
	g.out("")
}

func (g *codegen) emitContextStructs(p *Program) {
	for _, t := range p.Tasks {
		if t.Tier == "background" && len(t.CrossingVars) > 0 {
			g.out("// Context struct for task '" + t.Name + "' (lifted variables)")
			g.out("typedef struct {")
			for _, v := range findVarDeclsByName(t.Body, t.CrossingVars, nil) {
				g.out("    " + cTypeP(v.TypeName, "int32_t") + " " + cName(v.Name) + ";")
			}
			g.out("} " + t.Name + "_ctx_t;")
			g.out("static " + t.Name + "_ctx_t _" + t.Name + "_ctx = {0};")
			g.out("")
		}
	}
	for _, t := range p.Tasks {
		if t.Tier == "background" {
			g.out("static volt_task_t _" + t.Name + "_t = {0};")
		}
	}
	g.out("")
}

func (g *codegen) emitGlobalVars(p *Program) {
	if len(p.GlobalVars) == 0 {
		return
	}
	g.out("// Module-level variables")
	shared := findSharedVars(p)
	for _, v := range p.GlobalVars {
		if v.Shared {
			shared[v.Name] = true
		}
	}
	g.sharedVars = shared
	for _, v := range p.GlobalVars {
		ct := cTypeP(v.TypeName, "int32_t")
		if v.TypeName == nil || *v.TypeName == "" {
			ct = cType(g.varType(v.Name), "int32_t")
		}
		init := ""
		if v.Value != nil {
			init = " = " + g.expr(v.Value, "", nil)
		}
		if ct == "const char*" {
			delete(shared, v.Name)
		}
		if v.Shared && ct != "const char*" || shared[v.Name] {
			// Strings stay plain: a word-sized pointer store is already atomic on
			// every target, and _Atomic const char* breaks string APIs.
			g.out("static _Atomic " + ct + " " + cName(v.Name) + init + ";")
		} else {
			g.out("static " + ct + " " + cName(v.Name) + init + ";")
		}
	}
	g.out("")
}

// findSharedVars finds globals accessed from 2+ contexts (tasks, setup,
// loop; a critical task counts as preempting main).
func findSharedVars(p *Program) map[string]bool {
	globals := map[string]bool{}
	for _, v := range p.GlobalVars {
		globals[v.Name] = true
	}
	ctxs := map[string]map[string]bool{}
	add := func(stmts []Node, ctx ...string) {
		for name := range collectVarRefs(stmts) {
			if !globals[name] {
				continue
			}
			if ctxs[name] == nil {
				ctxs[name] = map[string]bool{}
			}
			for _, c := range ctx {
				ctxs[name][c] = true
			}
		}
	}
	for _, t := range p.Tasks {
		add(t.Body, t.Name)
	}
	if p.Setup != nil {
		add(p.Setup.Body, "setup")
	}
	if p.Loop != nil {
		add(p.Loop.Body, "loop")
	}
	for _, t := range p.Tasks {
		if t.Tier == "critical" {
			add(t.Body, "__critical__", "__main__")
		}
	}
	shared := map[string]bool{}
	for name, c := range ctxs {
		if len(c) > 1 {
			shared[name] = true
		}
	}
	return shared
}

func (g *codegen) emitStorageVars(p *Program) {
	if len(p.StorageVars) == 0 {
		return
	}
	g.out("// Storage variables (persisted via NVS)")
	for _, sv := range p.StorageVars {
		g.out("static " + cType(sv.TypeName, "int32_t") + " " + cName(sv.Name) + " = " + g.storageDefault(sv) + ";")
	}
	g.out("")
}

func (g *codegen) storageDefault(sv *StorageDecl) string {
	if sv.DefaultValue != nil {
		return g.expr(sv.DefaultValue, "", nil)
	}
	return "0"
}

func (g *codegen) emitChannels(p *Program) {
	if len(p.Channels) == 0 {
		return
	}
	g.out("// Inter-task channels")
	for _, ch := range p.Channels {
		ct := cType(ch.ElemType, "int32_t")
		c := strconv.Itoa(ch.Capacity)
		n := cName(ch.Name)
		idx := "uint8_t"
		if ch.Capacity > 65535 {
			idx = "uint32_t"
		} else if ch.Capacity > 255 {
			idx = "uint16_t"
		}
		g.out("typedef struct {")
		g.out("    " + ct + " buf[" + c + "];")
		g.out("    _Atomic " + idx + " head;")
		g.out("    _Atomic " + idx + " tail;")
		g.out("    _Atomic " + idx + " count;")
		g.out("} _ch_" + n + "_t;")
		g.out("static _ch_" + n + "_t _ch_" + n + " = {{0}, 0, 0, 0};")
		g.out("")
		g.out("static inline void " + n + "_send(" + ct + " val) {")
		g.out("    _ch_" + n + "_t *ch = &_ch_" + n + ";")
		g.out("    if (atomic_load(&ch->count) < " + c + ") {")
		g.out("        " + idx + " h = atomic_load(&ch->head);")
		g.out("        ch->buf[h] = val;")
		g.out("        atomic_store(&ch->head, (h + 1) % " + c + ");")
		g.out("        atomic_fetch_add(&ch->count, 1);")
		g.out("    }")
		g.out("}")
		g.out("static inline " + ct + " " + n + "_receive(void) {")
		g.out("    _ch_" + n + "_t *ch = &_ch_" + n + ";")
		g.out("    if (atomic_load(&ch->count) > 0) {")
		g.out("        " + idx + " t = atomic_load(&ch->tail);")
		g.out("        " + ct + " val = ch->buf[t];")
		g.out("        atomic_store(&ch->tail, (t + 1) % " + c + ");")
		g.out("        atomic_fetch_sub(&ch->count, 1);")
		g.out("        return val;")
		g.out("    }")
		g.out("    return 0;")
		g.out("}")
		g.out("static inline " + ct + " " + n + "_try_receive(void) { return " + n + "_receive(); }")
		g.out("static inline int " + n + "_count(void) { return atomic_load(&_ch_" + n + ".count); }")
		g.out("static inline bool " + n + "_has_data(void) { return atomic_load(&_ch_" + n + ".count) > 0; }")
		g.out("static inline bool " + n + "_is_empty(void) { return atomic_load(&_ch_" + n + ".count) == 0; }")
		g.out("static inline bool " + n + "_is_full(void) { return atomic_load(&_ch_" + n + ".count) >= " + c + "; }")
		g.out("static inline void " + n + "_clear(void) { atomic_store(&_ch_" + n + ".head, 0); atomic_store(&_ch_" + n + ".tail, 0); atomic_store(&_ch_" + n + ".count, 0); }")
		g.out("")
	}
}

// cPrefix is the C symbol prefix for a sensor type (registry aliases such
// as LCD16x2 -> hd44780), else the lowercased type.
func cPrefix(sensorType string) string {
	if d := Registry().Get(sensorType); d != nil && d.CPrefix != "" {
		return d.CPrefix
	}
	return strings.ToLower(sensorType)
}

func (g *codegen) emitSensorInstances(p *Program) {
	if p.Hardware == nil || len(p.Hardware.Sensors) == 0 {
		return
	}
	g.out("// Sensor instances")
	for _, s := range p.Hardware.Sensors {
		g.out("static " + cPrefix(s.SensorType) + "_t _" + s.Name + " = {0};")
	}
	g.out("")
}

func (g *codegen) emitFunctions(p *Program) {
	for _, f := range p.Functions {
		if f.ContainsYield {
			continue // yielding functions are inlined
		}
		g.out(g.funcSignature(f))
		g.locals = map[string]bool{}
		for _, p := range f.Params {
			g.locals[p.Name] = true
		}
		g.out("{")
		g.indent++
		g.stmts(f.Body, "", nil, false)
		g.indent--
		g.out("}")
		g.out("")
	}
}

func (g *codegen) emitTasks(p *Program) {
	for _, t := range p.Tasks {
		g.locals = map[string]bool{}
		switch t.Tier {
		case "background":
			hasCtx := len(t.CrossingVars) > 0
			g.out("// Background task: " + t.Name + " (protothread)")
			g.out("static void " + t.Name + "_run(volt_task_t *_vt)")
			g.out("{")
			g.indent++
			prefix := ""
			if hasCtx {
				g.out(t.Name + "_ctx_t *ctx = &_" + t.Name + "_ctx;")
				prefix = "ctx->"
			}
			g.out("VOLT_TASK_BEGIN(_vt);")
			g.stmts(t.Body, prefix, t.CrossingVars, false)
			g.out("VOLT_TASK_END(_vt);")
			g.indent--
			g.out("}")
			g.out("")
		case "priority":
			g.out("// Priority task: " + t.Name + " (FreeRTOS)")
			g.out("static void " + t.Name + "_run(void *arg)")
			g.out("{")
			g.indent++
			g.stmts(t.Body, "", nil, true)
			g.indent--
			g.out("}")
			g.out("")
		case "critical":
			g.out("// Critical task: " + t.Name + " (hardware timer ISR)")
			attr := ""
			if g.chipFamily == "esp32" || g.chipFamily == "esp8266" {
				attr = "IRAM_ATTR "
			}
			g.out("static void " + attr + t.Name + "_isr(void *arg)")
			g.out("{")
			g.indent++
			for _, s := range t.Body {
				if e, ok := s.(*EveryBlock); ok && e.Strict {
					g.stmts(e.Body, "", nil, false)
				}
			}
			g.indent--
			g.out("}")
			g.out("")
		}
	}
}

// ============================================================
// State machines
// ============================================================

func (g *codegen) emitMachine(m *MachineDecl, declared map[string]bool) {
	name := m.Name
	up := strings.ToUpper(name)
	g.currentMachine = name // for BecomeStmt; Python never resets it
	g.locals = map[string]bool{}
	states := m.States
	initial := states[0] // Python: next(..., states[0]) evaluates states[0] eagerly
	for _, s := range states {
		if s.IsInitial {
			initial = s
			break
		}
	}
	stateConst := func(s string) string { return up + "_" + strings.ToUpper(s) }

	events := map[string]bool{}
	for _, s := range states {
		for _, tr := range s.Transitions {
			if v, ok := tr.Event.(*VarRef); ok && !declared[v.Name] {
				events[v.Name] = true
			}
		}
	}
	if len(events) > 0 {
		g.out("// Event flags for " + name + " (set by external code, cleared after handling)")
		evs := make([]string, 0, len(events))
		for e := range events {
			evs = append(evs, e)
		}
		sort.Strings(evs)
		for _, e := range evs {
			declared[e] = true
			g.out("static _Atomic bool " + e + " = false;")
		}
		g.out("")
	}

	g.out("// State machine: " + name)
	g.out("typedef enum {")
	g.indent++
	for _, s := range states {
		g.out(stateConst(s.Name) + ",")
	}
	g.indent--
	g.out("} " + name + "_state_t;")
	g.out("")

	g.out("typedef struct {")
	g.indent++
	g.out(name + "_state_t current;")
	g.out("uint32_t state_entered_at;")
	for _, v := range m.Variables {
		g.out(cTypeP(v.TypeName, "int32_t") + " " + cName(v.Name) + ";")
	}
	// Retry counters of lifted try blocks live in the machine context.
	for _, tb := range machineLiftedTryBlocks(m) {
		g.out("int32_t _try_attempts_" + strconv.Itoa(tb.TryID) + ";")
	}
	g.indent--
	g.out("} " + name + "_ctx_t;")
	g.out("")

	g.out("static " + name + "_ctx_t _" + name + "_ctx = {")
	g.indent++
	g.out(".current = " + stateConst(initial.Name) + ",")
	for _, v := range m.Variables {
		val := "0"
		if v.Value != nil {
			val = g.expr(v.Value, "", nil)
		}
		g.out("." + cName(v.Name) + " = " + val + ",")
	}
	g.indent--
	g.out("};")
	g.out("static volt_task_t _" + name + "_t = {0};")
	// The first transition enters the initial state; there's no state to leave yet, so no exit block runs.
	g.out("static bool _" + name + "_started = false;")
	g.out("")

	g.out("static void " + name + "_transition(" + name + "_ctx_t *ctx, " + name + "_state_t new_state)")
	g.out("{")
	g.indent++
	mvars := make([]string, len(m.Variables))
	for i, v := range m.Variables {
		mvars[i] = v.Name
	}
	g.out("// Exit current state")
	g.out("if (_" + name + "_started) switch (ctx->current) {")
	g.indent++
	for _, s := range states {
		if len(s.ExitBody) > 0 {
			g.out("case " + stateConst(s.Name) + ":")
			g.indent++
			g.stmts(s.ExitBody, "ctx->", mvars, false)
			g.out("break;")
			g.indent--
		}
	}
	g.out("default: break;")
	g.indent--
	g.out("}")
	g.out("")
	g.out("_" + name + "_started = true;")
	g.out("ctx->current = new_state;")
	g.out("ctx->state_entered_at = volt_tick_ms();")
	g.out("")
	g.out("// Enter new state")
	g.out("switch (new_state) {")
	g.indent++
	for _, s := range states {
		if len(s.EnterBody) > 0 {
			g.out("case " + stateConst(s.Name) + ":")
			g.indent++
			g.stmts(s.EnterBody, "ctx->", mvars, false)
			g.out("break;")
			g.indent--
		}
	}
	g.out("default: break;")
	g.indent--
	g.out("}")
	g.indent--
	g.out("}")
	g.out("")

	g.out("// State machine task: " + name + " (runs as protothread)")
	g.out("static void " + name + "_run(volt_task_t *_vt)")
	g.out("{")
	g.indent++
	g.out(name + "_ctx_t *ctx = &_" + name + "_ctx;")
	g.out("VOLT_TASK_BEGIN(_vt);")
	g.out("")
	g.out("// Enter initial state")
	g.out(name + "_transition(ctx, " + stateConst(initial.Name) + ");")
	g.out("")
	g.out("for (;;) {")
	g.indent++
	g.out("uint32_t _elapsed = volt_tick_ms() - ctx->state_entered_at;")
	g.out("")
	g.out("switch (ctx->current) {")
	g.indent++
	for _, s := range states {
		if len(s.TimedTransitions) == 0 && len(s.Transitions) == 0 && len(s.Body) == 0 {
			continue
		}
		g.out("case " + stateConst(s.Name) + ":")
		g.indent++
		for _, tt := range s.TimedTransitions {
			ms := 0
			if d, ok := tt.Duration.(*DurationLiteral); ok {
				ms = msOf(d)
			}
			guard := ""
			if tt.Guard != nil {
				guard = " && (" + g.expr(tt.Guard, "ctx->", mvars) + ")"
			}
			g.out("if ((int32_t)(_elapsed - " + strconv.Itoa(ms) + ") >= 0" + guard + ") {")
			g.indent++
			if len(tt.Action) > 0 {
				g.stmts(tt.Action, "ctx->", mvars, false)
			}
			g.out(name + "_transition(ctx, " + stateConst(tt.TargetState) + ");")
			g.indent--
			g.out("}")
		}
		for _, tr := range s.Transitions {
			ev := g.expr(tr.Event, "ctx->", mvars)
			guard := ""
			if tr.Guard != nil {
				guard = " && (" + g.expr(tr.Guard, "ctx->", mvars) + ")"
			}
			g.out("if (" + ev + guard + ") {")
			g.indent++
			if v, ok := tr.Event.(*VarRef); ok {
				g.out("__atomic_store_n(&" + v.Name + ", false, __ATOMIC_SEQ_CST);")
			}
			if len(tr.Action) > 0 {
				g.stmts(tr.Action, "ctx->", mvars, false)
			}
			g.out(name + "_transition(ctx, " + stateConst(tr.TargetState) + ");")
			g.indent--
			g.out("}")
		}
		for _, st := range s.Body {
			g.stmt(st, "ctx->", mvars, false)
		}
		g.out("break;")
		g.indent--
	}
	g.out("default: break;")
	g.indent--
	g.out("}")
	g.out("")
	g.out("VOLT_YIELD_LABEL(_vt, 1, 1); // poll every 1ms")
	g.indent--
	g.out("}")
	g.out("VOLT_TASK_END(_vt);")
	g.indent--
	g.out("}")
	g.out("")
}

// machineLiftedTryBlocks lists try blocks whose retry counter lives in the
// machine context. Python's walker visits a TryBlock's body twice (once for
// the TryBlock, once via its `body` attr), so nested ones repeat; and it
// does not descend into an `else if`.
func machineLiftedTryBlocks(m *MachineDecl) []*TryBlock {
	var out []*TryBlock
	var walk func([]Node)
	walk = func(stmts []Node) {
		for _, s := range stmts {
			if tb, ok := s.(*TryBlock); ok {
				if tb.NeedsContextLift {
					out = append(out, tb)
				}
				walk(tb.Body)
			}
			switch s := s.(type) {
			case *EveryBlock:
				walk(s.Body)
			case *OnBlock:
				walk(s.Body)
			case *WhileStmt:
				walk(s.Body)
			case *ForStmt:
				walk(s.Body)
			case *TryBlock:
				walk(s.Body)
			case *TimeoutBlock:
				walk(s.TryBody)
				walk(s.FallbackBody)
			case *IfStmt:
				walk(s.ThenBody)
				if eb, ok := s.ElseBody.([]Node); ok {
					walk(eb)
				}
			}
		}
	}
	for _, s := range m.States {
		walk(s.EnterBody)
		walk(s.ExitBody)
		walk(s.Body)
	}
	return out
}

// ============================================================
// app_main
// ============================================================

var (
	i2cDefaults  = map[string][2]string{"esp32": {"21", "22"}, "esp8266": {"4", "5"}, "rp2040": {"4", "5"}, "stm32": {"7", "6"}}
	spiDefaults  = map[string][3]string{"esp32": {"23", "19", "18"}, "esp8266": {"13", "12", "14"}, "rp2040": {"19", "16", "18"}, "stm32": {"7", "6", "5"}}
	uartDefaults = map[string][2]string{"esp32": {"1", "3"}, "esp8266": {"1", "3"}, "rp2040": {"0", "1"}, "stm32": {"2", "3"}}
)

// softI2CBase is the first bus id of bit-banged I2C buses; hardware
// controllers are numbered from 0.
const softI2CBase = 100

var initPlaceholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func (g *codegen) emitAppMain(p *Program) {
	g.locals = map[string]bool{}
	chip := chipFamily(p)
	if chip == "rp2040" {
		g.out("int main(void)")
	} else {
		g.out("void app_main(void)")
	}
	g.out("{")
	g.indent++
	if chip == "rp2040" {
		g.out("stdio_init_all();")
	}
	if len(p.StorageVars) > 0 || g.usesStorage(p) {
		g.out("storage_init();")
	}
	g.out("volt_init();")
	g.out("")

	if hw := p.Hardware; hw != nil {
		g.out("// Hardware initialization")
		for _, pin := range hw.Pins {
			gpio := strconv.Itoa(pin.GPIO)
			switch pin.Mode {
			case "Output":
				g.out("pin_set_output(" + gpio + ");")
			case "Input":
				g.out("pin_set_input(" + gpio + ");")
			case "PullUpInput":
				g.out("pin_set_input_pullup(" + gpio + ");")
			case "PullDownInput":
				g.out("pin_set_input_pulldown(" + gpio + ");")
			case "PWM":
				g.out("pin_set_pwm(" + gpio + ");")
			case "Servo":
				g.out("servo_attach(" + gpio + ");")
			case "DAC":
				g.out("pin_set_dac(" + gpio + ");")
			case "Touch":
				g.out("touch_init(" + gpio + ");")
			}
		}
		g.out("")

		chip := g.chipFamily
		param := func(params Dict, key, def string) string {
			if v, ok := params.Vals[key]; ok {
				n, _ := v.(Node)
				return g.expr(n, "", nil)
			}
			return def
		}
		i2cBusIDs := map[string]int{}
		hwI2C, softI2C := 0, 0
		for _, bus := range hw.Buses {
			switch bus.BusType {
			case "i2c", "softi2c":
				var id int
				var kind string
				if bus.BusType == "softi2c" {
					id, kind = softI2CBase+softI2C, "soft/bit-bang"
					softI2C++
				} else {
					id, kind = hwI2C, "hw controller"
					hwI2C++
				}
				i2cBusIDs[bus.Name] = id
				d, ok := i2cDefaults[chip]
				if !ok {
					d = [2]string{"21", "22"}
				}
				sda := param(bus.Params, "sda", d[0])
				scl := param(bus.Params, "scl", d[1])
				speed := param(bus.Params, "speed", "400000")
				g.out(fmt.Sprintf("// I2C bus: %s (%s %d)", bus.Name, kind, id))
				g.out(fmt.Sprintf("volt_i2c_init(%d, %s, %s, %s);", id, sda, scl, speed))
			case "spi":
				d, ok := spiDefaults[chip]
				if !ok {
					d = [3]string{"23", "19", "18"}
				}
				mosi := param(bus.Params, "mosi", d[0])
				miso := param(bus.Params, "miso", d[1])
				clk := param(bus.Params, "clk", d[2])
				speed := param(bus.Params, "speed", "1000000")
				g.out("// SPI bus: " + bus.Name)
				g.out("volt_spi_init(0, " + mosi + ", " + miso + ", " + clk + ", " + speed + ");")
			case "uart", "serial":
				d, ok := uartDefaults[chip]
				if !ok {
					d = [2]string{"1", "3"}
				}
				tx := param(bus.Params, "tx", d[0])
				rx := param(bus.Params, "rx", d[1])
				baud := param(bus.Params, "baud", "9600")
				port := g.uartPorts[bus.Name]
				g.out(fmt.Sprintf("// UART/Serial: %s (port %d)", bus.Name, port))
				g.out(fmt.Sprintf("volt_uart_init(%d, %s, %s, %s);", port, tx, rx, baud))
			}
		}
		if len(hw.Buses) > 0 {
			g.out("")
		}

		busTypes := map[string]string{}
		for _, bus := range hw.Buses {
			busTypes[bus.Name] = strings.ToLower(bus.BusType)
		}
	sensors:
		for _, s := range hw.Sensors {
			stype := cPrefix(s.SensorType)
			dev := Registry().Get(s.SensorType)
			// Interface: the bus the device is on wins (a chip can be I2C
			// or SPI depending on wiring), then device.json, then i2c.
			iface, ok := busTypes[s.BusName]
			if !ok {
				iface = "i2c"
				if dev != nil {
					iface = strings.ToLower(dev.Interface)
				}
			}
			busID := i2cBusIDs[s.BusName]
			g.out("// Device: " + s.Name + " (" + s.SensorType + ")")
			tmpl := ""
			if dev != nil {
				tmpl = dev.InitArgs
			}
			switch {
			case tmpl != "":
				vals := map[string]string{
					"pin":     strconv.Itoa(s.Address),
					"addr":    strconv.Itoa(s.Address),
					"bus_id":  strconv.Itoa(busID),
					"uart_id": strconv.Itoa(g.uartPorts[s.BusName]),
				}
				if dev != nil {
					for i := range dev.Params {
						vals[dev.Params[i].Name] = paramValue(s, &dev.Params[i])
					}
				}
				for _, k := range s.PinParams.Keys {
					vals[k] = pyStr(s.PinParams.Vals[k])
				}
				// Only bare {identifier} placeholders are substituted: the
				// template comes from (possibly third-party) device.json.
				missing := ""
				args := initPlaceholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
					key := m[1 : len(m)-1]
					v, ok := vals[key]
					if !ok && missing == "" {
						missing = key
					}
					return v
				})
				if missing != "" {
					// Unreachable after the analyzer's pin-param check.
					g.out("// ERROR: missing " + missing + " pin for " + s.SensorType)
					continue sensors
				}
				g.out(stype + "_init(&_" + s.Name + ", " + args + ");")
			case iface == "spi":
				cs := namedPin(s, "cs")
				g.out(fmt.Sprintf("%s_init(&_%s, 0, %s);  // cs_pin=%s", stype, s.Name, cs, cs))
			case iface == "gpio":
				pin := namedPin(s, "pin")
				g.out(fmt.Sprintf("%s_init(&_%s, %s);  // pin=%s", stype, s.Name, pin, pin))
			case iface == "uart" || iface == "serial":
				g.out(stype + "_init(&_" + s.Name + ", 0);  // uart bus")
			default:
				g.out(fmt.Sprintf("%s_init(&_%s, %d, %d);  // bus %d, i2c_addr=0x%02x", stype, s.Name, busID, s.Address, busID, s.Address))
			}
		}
		if len(hw.Sensors) > 0 {
			g.out("")
		}
	}

	for _, sv := range p.StorageVars {
		def := g.storageDefault(sv)
		n := cName(sv.Name)
		switch sv.TypeName {
		case "float":
			g.out(n + ` = storage_load_float("` + sv.Name + `", ` + def + ");")
		case "string":
			g.out(`storage_load_str("` + sv.Name + `", ` + n + ", sizeof(" + n + "), " + def + ");")
		default:
			g.out(n + ` = storage_load_int("` + sv.Name + `", ` + def + ");")
		}
	}

	if p.Setup != nil {
		g.out("")
		g.out("// Setup")
		g.stmts(p.Setup.Body, "", nil, true)
	}

	for _, t := range p.Tasks {
		if t.Tier != "critical" {
			continue
		}
		for _, s := range t.Body {
			e, ok := s.(*EveryBlock)
			if !ok || !e.Strict {
				continue
			}
			us := 1000
			switch iv := e.Interval.(type) {
			case *DurationLiteral:
				us = iv.Microseconds
			case *IntLiteral:
				us = iv.Value * 1000
			}
			g.out(fmt.Sprintf(`volt_critical_create("%s", %s_isr, %d, NULL);`, t.Name, t.Name, us))
		}
	}
	for _, t := range p.Tasks {
		if t.Tier == "priority" {
			g.out(`volt_rt_create("` + t.Name + `", ` + t.Name + "_run, 4096, NULL, VOLT_PRIORITY_NORMAL, -1);")
		}
	}

	var bg []*TaskDecl
	for _, t := range p.Tasks {
		if t.Tier == "background" {
			bg = append(bg, t)
		}
	}
	if total := len(bg) + len(p.Machines); total > 0 {
		g.out("")
		g.out("// Start background tasks")
		g.out("volt_task_entry_t _tasks[] = {")
		g.indent++
		for _, t := range bg {
			g.out("{ " + t.Name + "_run, &_" + t.Name + "_t },")
		}
		for _, m := range p.Machines {
			g.out("{ " + m.Name + "_run, &_" + m.Name + "_t },")
		}
		g.indent--
		g.out("};")
		g.out(fmt.Sprintf("volt_run(_tasks, %d);", total))
	}

	if p.Loop != nil {
		g.out("")
		g.out("// Super-loop")
		g.out("while (1) {")
		g.indent++
		g.stmts(p.Loop.Body, "", nil, true)
		g.indent--
		g.out("}")
	}

	g.indent--
	g.out("}")
	g.out("")
}

// pyStr is Python's str() of a pin_params value (int, or an AST node).
func pyStr(v any) string {
	if i, ok := v.(int); ok {
		return strconv.Itoa(i)
	}
	return pyRepr(reflect.ValueOf(v))
}

// ============================================================
// Statements
// ============================================================

func (g *codegen) stmts(stmts []Node, ctx string, cv []string, freertos bool) {
	for _, s := range stmts {
		g.stmt(s, ctx, cv, freertos)
	}
}

// isErrorReturningCallStmt: an expression statement that is a call (other
// than sleep) may return volt_error_t and is error-checked inside try.
func isErrorReturningCallStmt(s Node) bool {
	es, ok := s.(*ExprStmt)
	if !ok {
		return false
	}
	switch e := es.Expr.(type) {
	case *FuncCall:
		return e.Name != "sleep"
	case *MethodCall:
		return true
	}
	return false
}

func (g *codegen) emitTry(s *TryBlock, ctx string, cv []string, freertos bool) {
	tid := s.TryID
	if tid == 0 {
		tid = g.tryCounter
	}
	g.tryCounter++
	id := strconv.Itoa(tid)
	endLabel := "_try_" + id + "_end"
	startLabel := "_try_" + id + "_start"

	g.lineDirective(s)
	g.out("{")
	g.indent++
	g.out("volt_error_t _try_err = VOLT_OK;")
	g.out("(void)_try_err;")

	bodyStmt := func(b Node, onErr string) {
		if isErrorReturningCallStmt(b) {
			call := g.expr(b.(*ExprStmt).Expr, ctx, cv)
			g.out("if ((_try_err = " + call + ") != VOLT_OK) goto " + onErr + ";")
		} else {
			g.stmt(b, ctx, cv, freertos)
		}
	}

	switch {
	case s.Policy == "skip":
		for _, b := range s.Body {
			bodyStmt(b, endLabel)
		}
		g.out(endLabel + ": ;")
	case s.Policy == "restart":
		for _, b := range s.Body {
			if isErrorReturningCallStmt(b) {
				call := g.expr(b.(*ExprStmt).Expr, ctx, cv)
				g.out("if ((_try_err = " + call + ") != VOLT_OK) { system_restart(); }")
			} else {
				g.stmt(b, ctx, cv, freertos)
			}
		}
		g.out(endLabel + ": ;")
	case s.Policy == "retry" && s.RetryLimit == nil:
		g.out(startLabel + ":")
		for _, b := range s.Body {
			bodyStmt(b, startLabel)
		}
		g.out(endLabel + ": ;")
	case s.Policy == "retry":
		// Bounded retry. The counter lives in the task context when the
		// body may yield (protothread re-entry resets the stack).
		checkLabel := "_try_" + id + "_check"
		counter := "_try_attempts_" + id
		if s.NeedsContextLift {
			prefix := ctx
			if prefix == "" {
				prefix = "ctx->"
			}
			counter = prefix + counter
			g.out(counter + " = 0;")
		} else {
			g.out("int32_t " + counter + " = 0;")
		}
		g.out(startLabel + ":")
		for _, b := range s.Body {
			bodyStmt(b, checkLabel)
		}
		g.out("goto " + endLabel + ";")
		g.out(checkLabel + ":")
		g.indent++
		g.out("if (++" + counter + " < " + strconv.Itoa(*s.RetryLimit) + ") goto " + startLabel + ";")
		switch deref(s.Terminal, "") {
		case "skip":
			g.out("goto " + endLabel + ";")
		case "restart":
			g.out("system_restart();")
		case "fail":
			g.out(`log_error("try", "exhausted retries");`)
			if !freertos {
				// background protothread: mark DONE so the scheduler stops
				g.out("_vt->status = VOLT_TASK_DONE;")
			}
			g.out("return;")
		}
		g.indent--
		g.out(endLabel + ": ;")
	}
	g.indent--
	g.out("}")
}

var atomicOps = map[string]string{
	"+=": "atomic_fetch_add", "-=": "atomic_fetch_sub", "|=": "atomic_fetch_or",
	"&=": "atomic_fetch_and", "^=": "atomic_fetch_xor",
}

func (g *codegen) stmt(s Node, ctx string, cv []string, freertos bool) {
	switch s := s.(type) {
	case *VarDecl:
		g.lineDirective(s)
		g.locals[s.Name] = true
		ct := "int32_t"
		if s.TypeName != nil && *s.TypeName != "" {
			ct = g.cType(*s.TypeName)
			g.varTypes[s.Name] = *s.TypeName
		}
		lifted := ctx != "" && slices.Contains(cv, s.Name)
		if or, ok := s.Value.(*OrExpr); ok {
			call := g.expr(or.Expr, ctx, cv)
			if lifted {
				g.out(ctx + cName(s.Name) + " = " + call + ";")
			} else {
				g.out(ct + " " + cName(s.Name) + " = " + call + ";")
			}
			switch or.Kind {
			case "skip":
				g.out("// or skip — iteration skipped on failure")
			case "default":
				g.out("// or " + g.expr(or.Fallback, ctx, cv) + " — fallback on failure")
			case "retry":
				g.out("// or retry(" + g.expr(or.Fallback, ctx, cv) + ")")
			}
		} else {
			val := g.expr(s.Value, ctx, cv)
			if lifted {
				g.out(ctx + cName(s.Name) + " = " + val + ";")
			} else {
				g.out(ct + " " + cName(s.Name) + " = " + val + ";")
			}
		}

	case *AssignExpr:
		g.lineDirective(s)
		target := g.expr(s.Target, ctx, cv)
		g.out(target + " = " + g.expr(s.Value, ctx, cv) + ";")

	case *CompoundAssign:
		g.lineDirective(s)
		target := g.expr(s.Target, ctx, cv)
		val := g.expr(s.Value, ctx, cv)
		v, isVar := s.Target.(*VarRef)
		if isVar && v.Name != "" && g.sharedVars[v.Name] {
			if op, ok := atomicOps[s.Op]; ok {
				g.out(op + "(&" + target + ", " + val + ");")
			} else {
				// No single atomic for *=, /=, ...: load-op-store.
				vt, ok := g.varTypes[v.Name]
				if !ok {
					vt = "int"
				}
				g.out("{ " + cType(vt, "int32_t") + " _tmp = atomic_load(&" + target + "); _tmp " + s.Op + " " + val + "; atomic_store(&" + target + ", _tmp); }")
			}
		} else {
			g.out(target + " " + s.Op + " " + val + ";")
		}

	case *BreakStmt:
		g.out("break;")

	case *ContinueStmt:
		g.out("continue;")

	case *BecomeStmt:
		if m := g.currentMachine; m != "" {
			g.out(m + "_transition(ctx, " + strings.ToUpper(m) + "_" + strings.ToUpper(s.Target) + ");")
		}

	case *ExprStmt:
		g.lineDirective(s)
		if or, ok := s.Expr.(*OrExpr); ok {
			call := g.expr(or.Expr, ctx, cv)
			switch or.Kind {
			case "retry":
				count := g.expr(or.Fallback, ctx, cv)
				g.out("{ volt_error_t _err = " + call + ";")
				g.indent++
				g.out("for (int _retry = 0; _err != VOLT_OK && _retry < " + count + "; _retry++) {")
				g.indent++
				g.out("_err = " + call + ";")
				g.indent--
				g.out("}")
				g.indent--
				g.out("}")
			case "skip":
				counter := "_skip_count_L" + strconv.Itoa(s.Region.StartLine)
				g.skipCounters[counter] = true
				g.out("if (" + call + " != VOLT_OK) { " + counter + "++; continue; }")
			case "restart":
				g.out("if (" + call + " != VOLT_OK) { system_restart(); }")
			default:
				g.out(call + ";")
			}
		} else if fc, ok := s.Expr.(*FuncCall); ok && fc.Name == "sleep" {
			g.expr(fc, ctx, cv) // evaluated for its side effects on buffer counters
			ms := "0"
			if len(fc.Args) > 0 {
				ms = g.expr(fc.Args[0], ctx, cv)
			}
			if freertos {
				g.out("volt_delay_ms(" + ms + ");")
			} else {
				label := 0
				if s.YieldLabel != nil {
					label = *s.YieldLabel
				}
				g.out("VOLT_YIELD_LABEL(_vt, " + strconv.Itoa(label) + ", " + ms + ");")
			}
		} else {
			g.out(g.expr(s.Expr, ctx, cv) + ";")
		}

	case *ReturnStmt:
		g.lineDirective(s)
		if s.Value != nil {
			g.out("return " + g.expr(s.Value, ctx, cv) + ";")
		} else {
			g.out("return;")
		}

	case *IfStmt:
		g.lineDirective(s)
		g.out("if (" + g.expr(s.Condition, ctx, cv) + ") {")
		g.indent++
		g.stmts(s.ThenBody, ctx, cv, freertos)
		g.indent--
		switch eb := s.ElseBody.(type) {
		case *IfStmt:
			if eb == nil {
				g.out("}")
				break
			}
			g.out("} else")
			g.stmt(eb, ctx, cv, freertos)
		case []Node:
			if len(eb) == 0 {
				g.out("}")
				break
			}
			g.out("} else {")
			g.indent++
			g.stmts(eb, ctx, cv, freertos)
			g.indent--
			g.out("}")
		default:
			g.out("}")
		}

	case *WhileStmt:
		g.lineDirective(s)
		g.out("while (" + g.expr(s.Condition, ctx, cv) + ") {")
		g.indent++
		g.stmts(s.Body, ctx, cv, freertos)
		g.indent--
		g.out("}")

	case *ForStmt:
		g.lineDirective(s)
		start := g.expr(s.Start, ctx, cv)
		end := g.expr(s.End, ctx, cv)
		v := cName(s.VarName)
		if s.Step != nil {
			step := g.expr(s.Step, ctx, cv)
			cmp := "<"
			if u, ok := s.Step.(*UnaryExpr); ok && u.Op == "-" {
				cmp = ">"
			} else if i, ok := s.Step.(*IntLiteral); ok && i.Value < 0 {
				cmp = ">"
			}
			g.out("for (int32_t " + v + " = " + start + "; " + v + " " + cmp + " " + end + "; " + v + " += " + step + ") {")
		} else {
			g.out("for (int32_t " + v + " = " + start + "; " + v + " < " + end + "; " + v + "++) {")
		}
		g.indent++
		g.stmts(s.Body, ctx, cv, freertos)
		g.indent--
		g.out("}")

	case *EveryBlock:
		if s.Strict {
			return // critical: body is emitted in the ISR function
		}
		ms := 0
		if d, ok := s.Interval.(*DurationLiteral); ok {
			ms = msOf(d)
		}
		g.out("for (;;) {")
		g.indent++
		if s.Guard != nil {
			g.out("if (" + g.expr(s.Guard, ctx, cv) + ") {")
			g.indent++
			g.stmts(s.Body, ctx, cv, freertos)
			g.indent--
			g.out("}")
		} else {
			g.stmts(s.Body, ctx, cv, freertos)
		}
		if freertos {
			g.out("volt_delay_ms(" + strconv.Itoa(ms) + ");")
		} else {
			g.out(fmt.Sprintf("VOLT_YIELD_LABEL(_vt, %d, %d);", s.YieldLabel, ms))
		}
		g.indent--
		g.out("}")

	case *OnBlock:
		ev := g.expr(s.Event, ctx, cv)
		g.out("// on " + ev)
		g.out(fmt.Sprintf("VOLT_WAIT_LABEL(_vt, %d, %s);", s.YieldLabel, ev))
		if s.Debounce != nil {
			ms := 50
			if d, ok := s.Debounce.(*DurationLiteral); ok {
				ms = msOf(d)
			}
			g.out(fmt.Sprintf("VOLT_YIELD_LABEL(_vt, %d, %d); // debounce", s.DebounceLabel, ms))
		}
		g.stmts(s.Body, ctx, cv, false) // Python drops use_freertos_delay here

	case *TimeoutBlock:
		g.lineDirective(s)
		ms := 0
		if d, ok := s.Duration.(*DurationLiteral); ok {
			ms = msOf(d)
		}
		g.out("{")
		g.indent++
		g.out("uint32_t _timeout_start = volt_tick_ms();")
		g.stmts(s.TryBody, ctx, cv, freertos)
		g.out(fmt.Sprintf("if ((int32_t)(volt_tick_ms() - _timeout_start - %d) >= 0) {", ms))
		g.indent++
		g.stmts(s.FallbackBody, ctx, cv, freertos)
		g.indent--
		g.out("}")
		g.indent--
		g.out("}")

	case *TryBlock:
		g.emitTry(s, ctx, cv, freertos)
	}
}

// ============================================================
// Expressions
// ============================================================

func (g *codegen) exprList(args []Node, ctx string, cv []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = g.expr(a, ctx, cv)
	}
	return strings.Join(parts, ", ")
}

func (g *codegen) expr(e Node, ctx string, cv []string) string {
	switch e := e.(type) {
	case nil:
		return "0"
	case *IntLiteral:
		return strconv.Itoa(e.Value)
	case *FloatLiteral:
		return pyRepr(reflect.ValueOf(e.Value)) + "f"
	case *BoolLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *StringLiteral:
		if strings.Contains(e.Value, "{") && strings.Contains(e.Value, "}") {
			return g.interpolatedString(e.Value, ctx, cv)
		}
		return `"` + e.Value + `"`
	case *DurationLiteral:
		return strconv.Itoa(msOf(e))
	case *VarRef:
		if c, ok := easingConstants[e.Name]; ok {
			return c
		}
		shadowed := g.locals[e.Name] || slices.Contains(cv, e.Name)
		for _, en := range g.enumNames {
			if !shadowed && slices.Contains(g.enumTypes[en], e.Name) {
				return strings.ToUpper(en) + "_" + strings.ToUpper(e.Name)
			}
		}
		if ctx != "" && slices.Contains(cv, e.Name) {
			return ctx + cName(e.Name)
		}
		return cName(e.Name)
	case *BinaryExpr:
		if e.Op == "+" && isStringConcat(e) {
			return g.stringConcat(e, ctx, cv)
		}
		left := g.expr(e.Left, ctx, cv)
		right := g.expr(e.Right, ctx, cv)
		if (e.Op == "==" || e.Op == "!=") && (g.inferValueType(e.Left) == "string" || g.inferValueType(e.Right) == "string") {
			// C would compare pointers
			return "(strcmp(" + left + ", " + right + ") " + e.Op + " 0)"
		}
		return "(" + left + " " + e.Op + " " + right + ")"
	case *UnaryExpr:
		return "(" + e.Op + g.expr(e.Operand, ctx, cv) + ")"
	case *OrExpr:
		// the fallback is handled at statement level
		return g.expr(e.Expr, ctx, cv)
	case *DotAccess:
		if v, ok := e.Obj.(*VarRef); ok {
			if _, isEnum := g.enumTypes[v.Name]; isEnum {
				return strings.ToUpper(v.Name) + "_" + strings.ToUpper(e.FieldName)
			}
		}
		return g.expr(e.Obj, ctx, cv) + "." + e.FieldName
	case *FuncCall:
		args := g.exprList(e.Args, ctx, cv)
		switch e.Name {
		case "run":
			return "/* run() handled by task table */"
		case "log":
			// log_info(tag, "%s", msg) keeps user text out of the format.
			tag := `"volt"`
			if len(e.Args) > 0 {
				tag = g.expr(e.Args[0], ctx, cv)
			}
			msg := `""`
			if len(e.Args) > 1 {
				msg = g.expr(e.Args[1], ctx, cv)
			}
			return `log_info(` + tag + `, "%s", ` + msg + ")"
		}
		if tmpl, ok := builtinMap[e.Name]; ok {
			if tmpl != "" {
				return pyFormat(&tmpl, map[string]string{"args": args})
			}
			return e.Name + "(" + args + ")"
		}
		return cName(e.Name) + "(" + args + ")"
	case *MethodCall:
		return g.methodCall(e, ctx, cv)
	case *ArrayLiteral:
		return "{" + g.exprList(e.Elements, ctx, cv) + "}"
	}
	return "/* unknown expr */"
}

func (g *codegen) methodCall(e *MethodCall, ctx string, cv []string) string {
	objName := ""
	if v, ok := e.Obj.(*VarRef); ok {
		objName = v.Name
	}
	args := g.exprList(e.Args, ctx, cv)

	if pin, ok := g.pinMap[objName]; ok && objName != "" {
		gpio := strconv.Itoa(pin.GPIO)
		if t, ok := lookupMethod(pin.Mode, e.Method); ok {
			return pyFormat(t, map[string]string{"gpio": gpio, "args": args})
		}
		base, _, _ := strings.Cut(pin.Mode, ".")
		if t, ok := lookupMethod(base, e.Method); ok {
			return pyFormat(t, map[string]string{"gpio": gpio, "args": args})
		}
	}

	if typeName, ok := g.builtinTypes[objName]; ok && objName != "" {
		if typeName == "Storage" {
			switch {
			case e.Method == "save" && len(e.Args) == 2:
				switch g.inferValueType(e.Args[1]) {
				case "float":
					return "storage_save_float(" + args + ")"
				case "string":
					return "storage_save_str(" + args + ")"
				}
				return "storage_save_int(" + args + ")"
			case e.Method == "load" && len(e.Args) >= 1:
				return "storage_load_int(" + args + ", 0)"
			case e.Method == "load_float" && len(e.Args) >= 1:
				return "storage_load_float(" + args + ", 0.0f)"
			case e.Method == "load_str" && len(e.Args) >= 1:
				// Rotating pool of 8 static buffers.
				idx := strconv.Itoa(g.storageStrBufCount % 8)
				g.storageStrBufCount++
				return "(storage_load_str(" + args + ", _volt_storage_strbuf[" + idx + "], " +
					"sizeof(_volt_storage_strbuf[" + idx + `]), ""), ` +
					"(const char*)_volt_storage_strbuf[" + idx + "])"
			case e.Method == "drain" && len(e.Args) >= 1:
				return "({ static int32_t _volt_drain_buf[64]; " +
					"int _drained = storage_drain(" + args + ", _volt_drain_buf, 64); " +
					"_drained; })"
			}
		}
		if t, ok := lookupMethod(typeName, e.Method); ok {
			return pyFormat(t, map[string]string{"args": args, "port": strconv.Itoa(g.uartPorts[objName])})
		}
	}

	if st, ok := g.sensorMap[objName]; ok && objName != "" {
		if t, ok := lookupMethod(st, e.Method); ok {
			return pyFormat(t, map[string]string{"sensor": "&_" + objName, "args": args, "bus_id": "0", "addr": "0"})
		}
	}

	return g.expr(e.Obj, ctx, cv) + "_" + e.Method + "(" + args + ")"
}

// ============================================================
// Helpers
// ============================================================

func (g *codegen) out(text string) {
	g.lines = append(g.lines, strings.Repeat("    ", g.indent)+text)
}

func (g *codegen) lineDirective(n Node) {
	if line := n.base().Region.StartLine; line > 0 {
		f := strings.ReplaceAll(g.sourceFile, `\`, `\\`)
		f = strings.ReplaceAll(f, `"`, `\"`)
		g.lines = append(g.lines, fmt.Sprintf(`#line %d "%s"`, line, f))
	}
}

// cType maps a Volt type to C, enum types included.
func (g *codegen) cType(typeName string) string {
	if _, ok := g.enumTypes[typeName]; ok {
		return strings.ToLower(typeName) + "_t"
	}
	return cType(typeName, "int32_t")
}

func (g *codegen) varType(name string) string {
	if t, ok := g.varTypes[name]; ok {
		return t
	}
	return "int"
}

// inferValueType infers the Volt type of an expression for storage.save.
func (g *codegen) inferValueType(e Node) string {
	switch e := e.(type) {
	case *IntLiteral, *DurationLiteral:
		return "int"
	case *FloatLiteral:
		return "float"
	case *StringLiteral:
		return "string"
	case *BoolLiteral:
		return "bool"
	case *VarRef:
		return g.varType(e.Name)
	case *MethodCall:
		objName := ""
		if v, ok := e.Obj.(*VarRef); ok {
			objName = v.Name
		}
		if st, ok := g.sensorMap[objName]; ok && objName != "" {
			if t, ok := lookupMethod(st, e.Method); ok {
				r := strings.ToLower(derefTemplate(t))
				if methodReturns(st, e.Method) == "string" {
					return "string"
				}
				if strings.Contains(r, "float") || strings.Contains(r, "lux") || strings.Contains(r, "temperature") {
					return "float"
				}
			}
		}
		if pin, ok := g.pinMap[objName]; ok && objName != "" && pin.Mode == "Analog" {
			return "int"
		}
		if tn, ok := g.builtinTypes[objName]; ok && objName != "" {
			if methodReturns(tn, e.Method) == "string" {
				return "string"
			}
			if t, ok := lookupMethod(tn, e.Method); ok {
				if strings.Contains(strings.ToLower(derefTemplate(t)), "float") {
					return "float"
				}
			}
		}
		return "int"
	case *FuncCall:
		if t, ok := g.funcReturnTypes[e.Name]; ok {
			return t
		}
		if sig, ok := builtinFuncs[e.Name]; ok && sig.Ret != "void" {
			return sig.Ret
		}
		return "int"
	case *BinaryExpr:
		l, r := g.inferValueType(e.Left), g.inferValueType(e.Right)
		if l == "float" || r == "float" {
			return "float"
		}
		if l == "string" || r == "string" {
			return "string"
		}
		return "int"
	case *UnaryExpr:
		return g.inferValueType(e.Operand)
	}
	return "int"
}

// methodReturns is a built-in or registry device method's declared Volt
// return type.
func methodReturns(typeName, method string) string {
	for _, m := range builtinMethodTypes[typeName] {
		if m.Name == method {
			return m.Ret
		}
	}
	ret := ""
	if dev := Registry().Get(typeName); dev != nil {
		for _, m := range dev.Methods {
			if m.Name == method {
				ret = m.Returns
			}
		}
	}
	return ret
}

func derefTemplate(t *string) string {
	if t == nil {
		panic(codegenPanic{"AttributeError: 'NoneType' object has no attribute 'lower'"})
	}
	return *t
}

// usesCalls reports whether any statement calls a method on obj.
func (g *codegen) usesCalls(p *Program, obj string) bool {
	return findCalls(g.programStmts(p), obj)
}

func (g *codegen) usesStorage(p *Program) bool {
	return len(p.StorageVars) > 0 || g.usesCalls(p, "storage")
}

func usesCriticalTask(p *Program) bool {
	return slices.ContainsFunc(p.Tasks, func(t *TaskDecl) bool { return t.Tier == "critical" })
}

// programStmts collects the statements of tasks, functions, machines (not
// timed-transition actions), setup and loop.
func (g *codegen) programStmts(p *Program) []Node {
	if g.allStmtsDone {
		return g.allStmts
	}
	var s []Node
	for _, t := range p.Tasks {
		s = append(s, t.Body...)
	}
	for _, f := range p.Functions {
		s = append(s, f.Body...)
	}
	for _, m := range p.Machines {
		for _, st := range m.States {
			s = append(s, st.EnterBody...)
			s = append(s, st.ExitBody...)
			s = append(s, st.Body...)
			for _, tr := range st.Transitions {
				s = append(s, tr.Action...)
			}
		}
	}
	if p.Setup != nil {
		s = append(s, p.Setup.Body...)
	}
	if p.Loop != nil {
		s = append(s, p.Loop.Body...)
	}
	g.allStmts, g.allStmtsDone = s, true
	return s
}

// ---- String concatenation and interpolation ----

func flattenConcat(e Node) []Node {
	if b, ok := e.(*BinaryExpr); ok && b.Op == "+" {
		return append(flattenConcat(b.Left), flattenConcat(b.Right)...)
	}
	return []Node{e}
}

// isStringConcat: a + chain mixing string literals with other values.
func isStringConcat(e *BinaryExpr) bool {
	hasStr, hasOther := false, false
	for _, p := range flattenConcat(e) {
		if _, ok := p.(*StringLiteral); ok {
			hasStr = true
		} else {
			hasOther = true
		}
	}
	return hasStr && hasOther
}

var (
	interpIntMarkers   = []string{".read()", ".free_heap()", ".uptime()", ".id()", "_count()", ".count()"}
	interpBoolMarkers  = []string{".is_connected()", ".is_on()", ".is_pressed()", ".is_empty()", ".is_full()", ".is_finger_present()", ".is_fault()"}
	interpFloatMarkers = []string{".temperature()", ".humidity()", ".pressure()", ".lux()", ".voltage()", ".current()", ".power()", ".altitude()", ".accel_x()", ".accel_y()", ".accel_z()", ".gyro_x()", ".gyro_y()", ".gyro_z()", ".ambient()", ".object()", ".gas()", ".uv()"}
)

func containsAny(s string, subs []string) bool {
	return slices.ContainsFunc(subs, func(m string) bool { return strings.Contains(s, m) })
}

// pyIsAlpha is Python's str.isalpha().
func pyIsAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// inferInterpType guesses an interpolated expression's printf type.
func (g *codegen) inferInterpType(s string) string {
	switch {
	case containsAny(s, interpIntMarkers):
		return "int"
	case containsAny(s, interpBoolMarkers):
		return "bool"
	case containsAny(s, interpFloatMarkers):
		return "float"
	}
	// Python looks the name up with '_' and '.' removed.
	name := strings.ReplaceAll(strings.ReplaceAll(s, "_", ""), ".", "")
	if pyIsAlpha(name) {
		if name == "true" || name == "false" {
			return "bool"
		}
		if t := g.varType(name); t != "int" {
			return t
		}
	}
	return "int"
}

var interpRe = regexp.MustCompile(`\{([^}]+)\}`)

// isBoolExpr: a comparison, a logical and / or, or a not.
func isBoolExpr(n Node) bool {
	switch e := n.(type) {
	case *BinaryExpr:
		switch e.Op {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||", "and", "or":
			return true
		}
	case *UnaryExpr:
		return e.Op == "!" || e.Op == "not"
	case *BoolLiteral:
		return true
	}
	return false
}

// simpleInterp is what the text-based interpolation path handles itself.
var simpleInterp = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*(\s*\.\s*[A-Za-z_][A-Za-z0-9_]*\s*\(\s*\))?\s*$`)

// pySplitGroups is re.split with one capturing group.
func pySplitGroups(re *regexp.Regexp, s string) []string {
	var parts []string
	prev := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		parts = append(parts, s[prev:m[0]], s[m[2]:m[3]])
		prev = m[1]
	}
	return append(parts, s[prev:])
}

func (g *codegen) nextStrBuf() string {
	idx := g.sprintfBufIdx
	g.sprintfBufIdx = (idx + 1) % 16
	return strconv.Itoa(idx)
}

func strBufExpr(idx, fmtStr, args string) string {
	if strings.Contains(fmtStr, "%s") {
		// Using snprintf's result keeps -Wformat-truncation quiet: a string
		// longer than the buffer is cut, by design.
		return "(snprintf(_volt_strbuf[" + idx + "], sizeof(_volt_strbuf[" + idx + `]), "` + fmtStr + `", ` + args + ") < 0 ? \"\" : _volt_strbuf[" + idx + "])"
	}
	return "(snprintf(_volt_strbuf[" + idx + "], sizeof(_volt_strbuf[" + idx + `]), "` + fmtStr + `", ` + args + "), _volt_strbuf[" + idx + "])"
}

// interpolatedString emits snprintf for "text {expr} more".
func (g *codegen) interpolatedString(text, ctx string, cv []string) string {
	parts := pySplitGroups(interpRe, text)
	if len(parts) <= 1 {
		return `"` + text + `"`
	}
	var fmts, args []string
	for i, part := range parts {
		if i%2 == 0 {
			fmts = append(fmts, strings.ReplaceAll(part, "%", "%%"))
			continue
		}
		es := strings.TrimSpace(part)
		c, typ := g.resolveInterpExpr(es, ctx, cv), g.inferInterpType(es)
		// Anything beyond `name` or `obj.method()` (int(x), a + b, f(x)…) is
		// parsed and emitted like any other expression.
		if !simpleInterp.MatchString(es) {
			if n, err := ParseExpr(es); err == nil {
				c, typ = g.expr(n, ctx, cv), g.inferValueType(n)
				if isBoolExpr(n) {
					typ = "bool" // printed as true / false
				}
			}
		}
		switch typ {
		case "float":
			fmts = append(fmts, "%.2f")
			args = append(args, "(double)("+c+")")
		case "bool":
			fmts = append(fmts, "%s")
			args = append(args, "("+c+` ? "true" : "false")`)
		case "string":
			fmts = append(fmts, "%s")
			args = append(args, c)
		default:
			fmts = append(fmts, "%ld")
			args = append(args, "(long)("+c+")")
		}
	}
	idx := g.nextStrBuf()
	f := strings.Join(fmts, "")
	if len(args) == 0 {
		return `"` + f + `"`
	}
	return strBufExpr(idx, f, strings.Join(args, ", "))
}

// resolveInterpExpr turns an interpolated `name` or `obj.method()` into C.
func (g *codegen) resolveInterpExpr(s, ctx string, cv []string) string {
	if strings.Contains(s, ".") && strings.Contains(s, "(") {
		obj, rest, _ := strings.Cut(s, ".")
		obj = strings.TrimSpace(obj)
		method := strings.TrimRight(strings.TrimSpace(rest), "()")
		if tn, ok := g.builtinTypes[obj]; ok {
			if t, ok := lookupMethod(tn, method); ok {
				return pyFormat(t, map[string]string{"args": "", "port": strconv.Itoa(g.uartPorts[obj])})
			}
		}
		if pin, ok := g.pinMap[obj]; ok {
			base, _, _ := strings.Cut(pin.Mode, ".")
			if t, ok := lookupMethod(base, method); ok {
				return pyFormat(t, map[string]string{"gpio": strconv.Itoa(pin.GPIO), "args": ""})
			}
		}
		if st, ok := g.sensorMap[obj]; ok {
			if t, ok := lookupMethod(st, method); ok {
				return pyFormat(t, map[string]string{"sensor": "&_" + obj, "args": ""})
			}
			return cPrefix(st) + "_" + method + "(&_" + obj + ")"
		}
		return obj + "_" + method + "()"
	}
	name := strings.TrimSpace(s)
	if len(cv) > 0 && slices.Contains(cv, name) && ctx != "" {
		return ctx + cName(name)
	}
	return cName(name)
}

var (
	concatBoolMethods = []string{"is_connected", "is_on", "is_pressed", "is_empty", "is_full", "is_finger_present", "is_fault"}
	concatIntMethods  = []string{"free_heap", "read", "list_count", "uptime", "id"}
)

// stringConcat emits snprintf for string + int/float/bool concatenation.
func (g *codegen) stringConcat(e *BinaryExpr, ctx string, cv []string) string {
	var fmts, args []string
	for _, part := range flattenConcat(e) {
		switch p := part.(type) {
		case *StringLiteral:
			fmts = append(fmts, strings.ReplaceAll(p.Value, "%", "%%"))
		case *IntLiteral:
			fmts = append(fmts, "%ld")
			args = append(args, "(long)"+strconv.Itoa(p.Value))
		case *FloatLiteral:
			fmts = append(fmts, "%.2f")
			args = append(args, pyRepr(reflect.ValueOf(p.Value))+"f")
		case *BoolLiteral:
			fmts = append(fmts, "%s")
			if p.Value {
				args = append(args, `"true"`)
			} else {
				args = append(args, `"false"`)
			}
		default:
			c := g.expr(part, ctx, cv)
			mc, isMC := part.(*MethodCall)
			_, isBin := part.(*BinaryExpr)
			switch {
			case isMC && slices.Contains(concatBoolMethods, mc.Method):
				fmts = append(fmts, "%s")
				args = append(args, "("+c+` ? "true" : "false")`)
			case isMC && slices.Contains(concatIntMethods, mc.Method):
				fmts = append(fmts, "%ld")
				args = append(args, "(long)"+c)
			case g.inferValueType(part) == "string":
				fmts = append(fmts, "%s")
				args = append(args, c)
			case g.inferValueType(part) == "float":
				fmts = append(fmts, "%.2f")
				args = append(args, "(double)("+c+")")
			case g.inferValueType(part) == "bool":
				fmts = append(fmts, "%s")
				args = append(args, "(("+c+`) ? "true" : "false")`)
			case isBin:
				fmts = append(fmts, "%ld")
				args = append(args, "(long)("+c+")")
			default:
				fmts = append(fmts, "%ld")
				args = append(args, "(long)"+c)
			}
		}
	}
	idx := g.nextStrBuf()
	return strBufExpr(idx, strings.Join(fmts, ""), strings.Join(args, ", "))
}

// ============================================================
// codegen_utils.py
// ============================================================

var strInterpRefRe = regexp.MustCompile(`\{([\p{L}\p{N}_]+)(?:\.[\p{L}\p{N}_]+\(\))?\}`)

// collectVarRefs collects the variable names referenced in stmts.
func collectVarRefs(stmts []Node) map[string]bool {
	names := map[string]bool{}
	var st func([]Node)
	var ex func(Node)
	ex = func(e Node) {
		switch e := e.(type) {
		case *VarRef:
			names[e.Name] = true
		case *BinaryExpr:
			ex(e.Left)
			ex(e.Right)
		case *UnaryExpr:
			ex(e.Operand)
		case *FuncCall:
			for _, a := range e.Args {
				ex(a)
			}
		case *MethodCall:
			ex(e.Obj)
			for _, a := range e.Args {
				ex(a)
			}
		case *AssignExpr:
			if v, ok := e.Target.(*VarRef); ok {
				names[v.Name] = true
			}
			ex(e.Value)
		case *StringLiteral:
			for _, m := range strInterpRefRe.FindAllStringSubmatch(e.Value, -1) {
				names[m[1]] = true
			}
		case *DotAccess:
			ex(e.Obj)
		}
	}
	st = func(stmts []Node) {
		for _, s := range stmts {
			switch s := s.(type) {
			case *VarDecl:
				ex(s.Value)
			case *AssignExpr:
				if v, ok := s.Target.(*VarRef); ok {
					names[v.Name] = true
				}
				ex(s.Value)
			case *CompoundAssign:
				if v, ok := s.Target.(*VarRef); ok {
					names[v.Name] = true
				}
				ex(s.Value)
			case *ExprStmt:
				ex(s.Expr)
			case *ReturnStmt:
				ex(s.Value)
			case *IfStmt:
				ex(s.Condition)
				st(s.ThenBody)
				switch eb := s.ElseBody.(type) {
				case []Node:
					st(eb)
				case *IfStmt:
					if eb != nil {
						st([]Node{eb})
					}
				}
			case *WhileStmt:
				ex(s.Condition)
				st(s.Body)
			case *ForStmt:
				ex(s.Start)
				ex(s.End)
				ex(s.Step)
				st(s.Body)
			case *EveryBlock:
				ex(s.Interval)
				st(s.Body)
			case *OnBlock:
				ex(s.Event)
				st(s.Body)
			case *TimeoutBlock:
				ex(s.Duration)
				st(s.TryBody)
				st(s.FallbackBody)
			}
		}
	}
	st(stmts)
	return names
}

// subBodies lists a compound statement's child statement lists.
func subBodies(s Node) [][]Node {
	switch s := s.(type) {
	case *EveryBlock:
		return [][]Node{s.Body}
	case *OnBlock:
		return [][]Node{s.Body}
	case *IfStmt:
		out := [][]Node{s.ThenBody}
		switch eb := s.ElseBody.(type) {
		case []Node:
			out = append(out, eb)
		case *IfStmt:
			if eb != nil {
				out = append(out, []Node{eb})
			}
		}
		return out
	case *WhileStmt:
		return [][]Node{s.Body}
	case *ForStmt:
		return [][]Node{s.Body}
	case *TimeoutBlock:
		return [][]Node{s.TryBody, s.FallbackBody}
	case *TryBlock:
		return [][]Node{s.Body}
	case *MatchStmt:
		var out [][]Node
		for _, a := range s.Arms {
			if b, ok := a.Body.([]Node); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
}

// findVarDeclsByName finds VarDecls named in names, recursively, in order.
func findVarDeclsByName(stmts []Node, names []string, out []*VarDecl) []*VarDecl {
	for _, s := range stmts {
		if v, ok := s.(*VarDecl); ok && slices.Contains(names, v.Name) {
			out = append(out, v)
		}
		for _, b := range subBodies(s) {
			out = findVarDeclsByName(b, names, out)
		}
	}
	return out
}

// findCalls reports whether any statement (recursively) calls a method on obj.
func findCalls(stmts []Node, obj string) bool {
	for _, s := range stmts {
		if stmtCallsObj(s, obj) {
			return true
		}
		for _, b := range subBodies(s) {
			if findCalls(b, obj) {
				return true
			}
		}
	}
	return false
}

func stmtCallsObj(s Node, obj string) bool {
	switch s := s.(type) {
	case *ExprStmt:
		return exprCallsObj(s.Expr, obj)
	case *VarDecl:
		return exprCallsObj(s.Value, obj)
	case *AssignExpr:
		return exprCallsObj(s.Target, obj) || exprCallsObj(s.Value, obj)
	case *CompoundAssign:
		return exprCallsObj(s.Target, obj) || exprCallsObj(s.Value, obj)
	case *ReturnStmt:
		return exprCallsObj(s.Value, obj)
	case *IfStmt:
		return exprCallsObj(s.Condition, obj)
	case *WhileStmt:
		return exprCallsObj(s.Condition, obj)
	case *ForStmt:
		return exprCallsObj(s.Start, obj) || exprCallsObj(s.End, obj) || exprCallsObj(s.Step, obj)
	case *EveryBlock:
		return exprCallsObj(s.Guard, obj) || exprCallsObj(s.Interval, obj)
	case *OnBlock:
		return exprCallsObj(s.Event, obj)
	case *TimeoutBlock:
		return exprCallsObj(s.Duration, obj)
	case *MatchStmt:
		return exprCallsObj(s.Expr, obj)
	}
	return false
}

func exprCallsObj(e Node, obj string) bool {
	switch e := e.(type) {
	case *MethodCall:
		if v, ok := e.Obj.(*VarRef); ok && v.Name == obj {
			return true
		}
		return exprCallsObj(e.Obj, obj) || slices.ContainsFunc(e.Args, func(a Node) bool { return exprCallsObj(a, obj) })
	case *BinaryExpr:
		return exprCallsObj(e.Left, obj) || exprCallsObj(e.Right, obj)
	case *UnaryExpr:
		return exprCallsObj(e.Operand, obj)
	case *OrExpr:
		return exprCallsObj(e.Expr, obj) || exprCallsObj(e.Fallback, obj)
	case *FuncCall:
		return slices.ContainsFunc(e.Args, func(a Node) bool { return exprCallsObj(a, obj) })
	case *AssignExpr:
		return exprCallsObj(e.Target, obj) || exprCallsObj(e.Value, obj)
	case *DotAccess:
		return exprCallsObj(e.Obj, obj)
	}
	return false
}
