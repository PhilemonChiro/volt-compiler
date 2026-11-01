package compiler

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Port of hardware.py — pin, bus and sensor validation against the chip's
// pin database. With no (known) chip, the legacy ESP32 table is used.

func validateHardware(program *Program, errs *Errors, chip chipSpec) {
	hw := program.Hardware
	if hw == nil {
		return
	}
	if chip != nil {
		validateCapabilities(program, chip, errs)
	}

	// Pins by GPIO, for conflict detection. Python keys the chip path by
	// str(gpio) and the legacy path by int, and its bus check only saw
	// chip-path pins; legacy pins are copied over so both paths catch
	// pin/bus clashes.
	usedPins := map[string]*PinDecl{}
	legacyUsed := map[int]*PinDecl{}
	h := &hwCheck{errs: errs, caps: legacyCaps, taken: takenGPIOs(hw),
		usesWifi: containsMethodCall(program, "wifi", "connect")}
	if chip != nil {
		h.caps = chip.caps()
	}
	for _, pin := range hw.Pins {
		if chip != nil {
			h.validatePinChip(pin, usedPins, chip)
		} else {
			h.validatePinLegacy(pin, legacyUsed)
		}
	}

	h.validateDevices(program, chip, usedPins, legacyUsed)
	for g, p := range legacyUsed {
		usedPins[strconv.Itoa(g)] = p
	}
	h.validateBusPinConflicts(program, usedPins)
	h.validateBusPinCaps(program, chip)

	busKinds := map[string]string{}
	for _, b := range hw.Buses {
		busKinds[b.Name] = strings.ToLower(b.BusType)
	}
	usedAddrs := map[string]map[int]*SensorDecl{}
	for _, s := range hw.Sensors {
		if Registry().Get(s.SensorType) == nil {
			continue // reported by the resolver
		}
		for _, c := range ProjectConflicts {
			if c == s.SensorType {
				errs.Warning(s.TypeRegion, "HARDWARE WARNING", fmt.Sprintf("This uses the shared %s library, not the one in this project's devices/ folder.", c),
					"A project can't replace a shared library. To add methods this project needs, give the project library its own name and c_prefix (e.g. "+strings.ToLower(c)+"_app).")
			}
		}
		validateSensor(s, usedAddrs, busKinds, errs)
		if busKinds[s.BusName] == "i2c" {
			validateAddressList(s, errs)
		}
	}
	for _, b := range hw.Buses {
		validateBus(b, errs)
	}
	// The chip specs have no extra hardware rules of their own.
	if chip == nil {
		validateDeepSleepPins(program, errs)
		validateSPISpeed(program, errs)
	}
}

func validateCapabilities(program *Program, chip chipSpec, errs *Errors) {
	const hint = "Chips with WiFi: esp32, esp8266"
	if containsMethodCall(program, "wifi", "connect") && !chip.supports("wifi") {
		errs.Error(program.Region, "HARDWARE ERROR", chip.displayName()+" does not have WiFi hardware.", hint)
	}
	if containsMethodCall(program, "mqtt", "connect") && !chip.supports("wifi") {
		errs.Error(program.Region, "HARDWARE ERROR", fmt.Sprintf("MQTT requires WiFi, but %s has no WiFi.", chip.displayName()), hint)
	}
	if containsMethodCall(program, "http", "get") && !chip.supports("wifi") {
		errs.Error(program.Region, "HARDWARE ERROR", fmt.Sprintf("HTTP requires WiFi, but %s has no WiFi.", chip.displayName()), hint)
	}
}

// hwCheck carries what pin diagnostics need to suggest fixes.
type hwCheck struct {
	errs     *Errors
	caps     pinCaps
	taken    map[int]bool
	usesWifi bool
}

// report adds a hardware diagnostic; with move set, it offers moving the
// number at at (a pin or bus signal) to free GPIOs that suit mode.
func (h *hwCheck) report(sev, cat, code string, r Region, msg, hint string, move bool, at Region, mode string) {
	d := Diagnostic{Region: r, Category: cat, Message: msg, Hint: hint, Severity: sev, Code: code}
	if move {
		d.Fixes = moveFixes(at, h.caps.suggestPins(mode, h.taken, h.usesWifi, 3))
	}
	h.errs.add(d)
}

func (h *hwCheck) pinError(pin *PinDecl, code, msg, hint string) {
	h.report("error", "HARDWARE ERROR", code, pin.Region, msg, hint, true, pin.GPIORegion, pin.Mode)
}

func (h *hwCheck) pinWarning(pin *PinDecl, code, msg, hint string) {
	h.report("warning", "HARDWARE WARNING", code, pin.Region, msg, hint, true, pin.GPIORegion, pin.Mode)
}

func (h *hwCheck) validatePinChip(pin *PinDecl, used map[string]*PinDecl, chip chipSpec) {
	name := strconv.Itoa(pin.GPIO)
	issues := chip.validatePin(name, pin.Mode)
	failed := false
	for _, is := range issues {
		if is.severity == "error" {
			h.pinError(pin, is.code, is.message, is.hint)
			failed = true
		} else {
			h.pinWarning(pin, is.code, is.message, is.hint)
		}
	}
	if failed {
		return
	}
	if first, ok := used[name]; ok {
		h.pinError(pin, "pin-conflict",
			fmt.Sprintf("Pin %s is already used by `%s` (line %d).", name, first.Name, first.Region.StartLine),
			"Each pin can only be used once.")
		return
	}
	used[name] = pin
	if pin.Mode == "Analog" && h.usesWifi && slices.Contains(chip.adc2GPIOs(), pin.GPIO) {
		h.pinWarning(pin, "pin-adc2-wifi",
			fmt.Sprintf("Pin %s is on ADC2, which is blocked while WiFi is active. Analog reads will return errors.", name),
			"Use ADC1 pins instead.")
	}
}

func (h *hwCheck) validatePinLegacy(pin *PinDecl, used map[int]*PinDecl) {
	gpio, r := pin.GPIO, pin.Region
	info := legacyPins[gpio]
	safe := joinInts(legacySafeOutput, "")
	if info == nil {
		h.pinError(pin, "pin-unknown", fmt.Sprintf("GPIO %d does not exist on ESP32 (valid: 0-39).", gpio), "")
		return
	}
	if info.Blocked {
		h.pinError(pin, "pin-blocked", info.BlockedReason, "Available output pins: "+safe)
		return
	}
	if first, ok := used[gpio]; ok {
		h.pinError(pin, "pin-conflict",
			fmt.Sprintf("GPIO %d is already used by pin `%s` (line %d).", gpio, first.Name, first.Region.StartLine),
			"Each GPIO can only be used once.")
		return
	}
	used[gpio] = pin

	mode := pin.Mode
	switch {
	case mode == "Output" && !info.CanOutput:
		h.pinError(pin, "pin-input-only", fmt.Sprintf("GPIO %d is input-only — cannot be used as Output.", gpio),
			"Input-only pins: 34-39. Available output pins: "+safe)
		return
	case (mode == "PWM" || mode == "Servo") && !info.CanPWM:
		h.pinError(pin, "pin-input-only", fmt.Sprintf("GPIO %d cannot be used as %s (input-only pin).", gpio, mode), "")
		return
	case mode == "DAC" && !slices.Contains(esp32DACPins, gpio):
		h.pinError(pin, "pin-no-dac", fmt.Sprintf("GPIO %d has no DAC channel — cannot be used as DAC.", gpio), "DAC pins: 25, 26")
		return
	case mode == "Touch" && !slices.Contains(esp32TouchPins, gpio):
		h.pinError(pin, "pin-no-touch", fmt.Sprintf("GPIO %d is not a touch pin.", gpio),
			"Touch pins: "+joinInts(esp32TouchPins, ""))
		return
	case mode == "Analog" && !info.CanAnalog:
		h.pinError(pin, "pin-no-adc", fmt.Sprintf("GPIO %d does not have an ADC channel — cannot be used as Analog.", gpio),
			"ADC1 (safe with WiFi): GPIO 32-39. ADC2 (blocked by WiFi): GPIO 0, 2, 4, 12-15, 25-27.")
		return
	case strings.Contains(mode, "PullUp") && !info.HasInternalPullup:
		h.pinError(pin, "pin-no-pullup", fmt.Sprintf("GPIO %d has no internal pull-up resistor (input-only pins 34-39).", gpio),
			"Use an external pull-up resistor, or use a different pin.")
		return
	case strings.Contains(mode, "PullDown") && !info.HasInternalPulldown:
		h.pinError(pin, "pin-no-pulldown", fmt.Sprintf("GPIO %d has no internal pull-down resistor (input-only pins 34-39).", gpio),
			"Use an external pull-down resistor, or use a different pin.")
		return
	}

	if info.IsStrapping && info.StrappingWarning != "" && !esp32LEDPin(gpio, mode) {
		h.pinWarning(pin, "pin-strapping", fmt.Sprintf("GPIO %d: %s", gpio, info.StrappingWarning), "")
	}
	if info.IsUART0 {
		h.pinWarning(pin, "pin-uart0",
			fmt.Sprintf("GPIO %d is used by UART0 (USB serial). Using it will break serial monitor and firmware flashing.", gpio),
			"Use a different pin unless you specifically intend to disable serial output.")
	}
	if mode == "Analog" && h.usesWifi && slices.Contains(legacyADC2, gpio) {
		h.pinWarning(pin, "pin-adc2-wifi",
			fmt.Sprintf("GPIO %d is on ADC2, which is BLOCKED while WiFi is active. Analog reads will return errors or garbage.", gpio),
			"Use ADC1 pins instead (GPIO 32-39) which work with WiFi.")
	}
	if gpio == 36 || gpio == 39 {
		h.pinWarning(pin, "pin-errata",
			fmt.Sprintf("GPIO %d has a hardware errata: interrupts may fire spuriously during WiFi/BT light sleep. Use a different pin for interrupt-driven input.", gpio),
			"GPIO 34, 35 do not have this errata.")
	}
	if info.IsJTAG {
		h.pinWarning(pin, "pin-jtag", fmt.Sprintf("GPIO %d is a JTAG pin. Using it disables JTAG debugging.", gpio), "")
	}
	for _, note := range info.Notes {
		// Only a note that changes whether the pin can be used is a warning: a pin most boards don't break
		// out. The rest stay in the pin's details: what else the pin can do (DAC, 32 kHz crystal), the
		// WROVER's PSRAM (Volt targets WROOM modules), and notes already warned about (UART0, errata).
		if noteCatch(note) != "not on DevKit" {
			continue
		}
		h.errs.add(Diagnostic{Region: r, Category: "HARDWARE NOTE", Message: fmt.Sprintf("GPIO %d: %s", gpio, note),
			Severity: "warning", Code: "pin-note"})
	}
}

// unknownDevice reports a device type with no library on this computer: a
// typo, or a library that comes from the Grid and isn't installed yet.
func unknownDevice(s *SensorDecl, errs *Errors) {
	hint := fmt.Sprintf("Install it from the Grid: Volt IDE → Library → Discover → %s.", s.SensorType)
	var names []string
	for _, d := range Registry().Devices {
		names = append(names, d.Name)
	}
	if near, ok := suggestSimilar(s.SensorType, names, 2); ok {
		hint = fmt.Sprintf("Did you mean `%s`? If not, install %s from the Grid: Volt IDE → Library → Discover.", near, s.SensorType)
	}
	for _, c := range ProjectConflicts {
		if c == s.SensorType {
			hint = fmt.Sprintf("This project's devices/ folder has a %s library, but it uses the name or C prefix of a shared library, which a project can't replace. Rename it (name and c_prefix).", c)
		}
	}
	errs.add(Diagnostic{Region: s.TypeRegion, Category: "HARDWARE ERROR", Severity: "error", Code: "unknown-device",
		Message: fmt.Sprintf("`%s` isn't installed on this computer.", s.SensorType), Hint: hint})
}

// validateSensor checks I2C address range and per-bus uniqueness (and, on
// legacy SPI/UART buses, uniqueness of the `at` number). GPIO devices and
// SPI devices with a named select pin have no address to check.
func validateSensor(s *SensorDecl, used map[string]map[int]*SensorDecl, busKinds map[string]string, errs *Errors) {
	bus, addr := s.BusName, s.Address
	if busKinds[bus] != "i2c" && (!s.HasAddress || len(s.PinParams.Keys) > 0) || bus == "" {
		return
	}
	busText := fmt.Sprintf("bus `%s`", bus)
	if !s.HasBus {
		busText = "the default I2C bus"
	}
	// 0x00..0x07 and 0x78..0x7F are reserved by the I2C spec.
	if busKinds[bus] == "i2c" && (addr < 0x08 || addr > 0x77) {
		errs.add(Diagnostic{Region: s.Region, Category: "HARDWARE ERROR", Severity: "error", Code: "i2c-address-range",
			Message: fmt.Sprintf("I2C address 0x%02X on %s is outside the 7-bit device range 0x08..0x77.", addr, busText),
			Hint:    "0x00..0x07 and 0x78..0x7F are reserved by the I²C spec. Check the device datasheet — most parts have a fixed address or an A0/A1 strap option."})
		return
	}
	if used[bus] == nil {
		used[bus] = map[int]*SensorDecl{}
	}
	if first, ok := used[bus][addr]; ok {
		// Quick fixes: the device's other selectable addresses still free on this bus.
		var fixes []Fix
		if dev := Registry().Get(s.SensorType); dev != nil {
			for _, a := range dev.Addresses {
				v, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(a), "0x"), 16, 32)
				if _, taken := used[bus][int(v)]; err != nil || taken || int(v) == addr || len(fixes) == 3 {
					continue
				}
				fix := Fix{Title: fmt.Sprintf("Use 0x%02X", v), Region: s.AddressRegion, Text: fmt.Sprintf("0x%02X", v)}
				if !s.HasAddress { // default address: write one
					fix.Region, fix.Text = s.EndRegion, " at "+fix.Text
				}
				fixes = append(fixes, fix)
			}
		}
		hint := "Each I2C address must be unique per bus. Check the sensor datasheet for alternative addresses."
		if len(fixes) > 0 {
			hint = "Each I2C address must be unique per bus. Pick another address below and set the board's address pin (ADDR / SDO / A0) to match."
		}
		errs.add(Diagnostic{Region: s.Region, Category: "HARDWARE ERROR", Severity: "error", Code: "i2c-address-clash",
			Message: fmt.Sprintf("I2C address 0x%02X on %s is already used by `%s` (line %d).", addr, busText, first.Name, first.Region.StartLine),
			Hint:    hint, Fixes: fixes})
		return
	}
	used[bus][addr] = s
}

var busSignalParams = []string{"sda", "scl", "mosi", "miso", "clk", "sck", "cs", "tx", "rx"}

// busParamGPIO returns an integer-literal bus parameter's value.
func busParamGPIO(v any) (int, bool) {
	if lit, ok := v.(*IntLiteral); ok {
		return lit.Value, true
	}
	return 0, false
}

// validateBusPinConflicts: a GPIO can't be both a `pin` and a bus signal,
// nor a signal of two buses.
func (h *hwCheck) validateBusPinConflicts(program *Program, usedPins map[string]*PinDecl) {
	buses := program.Hardware.Buses
	for _, bus := range buses {
		for _, pname := range bus.Params.Keys {
			gpio, ok := busParamGPIO(bus.Params.Vals[pname])
			if !ok || !slices.Contains(busSignalParams, pname) {
				continue
			}
			if first, ok := usedPins[strconv.Itoa(gpio)]; ok {
				h.report("error", "HARDWARE ERROR", "bus-pin-conflict", bus.Region,
					fmt.Sprintf("GPIO %d is used by %s (%s) but was already declared as pin `%s` (line %d).",
						gpio, busLabel(bus), pname, first.Name, first.Region.StartLine),
					"A pin can only belong to one subsystem. Either remove the `pin` declaration or move the bus signal to a free GPIO.",
					sigMovable(bus, pname), regionOf(bus.Params.Vals[pname].(Node)), "Output")
			}
		}
	}
	type owner struct {
		bus, role string
		line      int
	}
	owners := map[int]owner{}
	for _, bus := range buses {
		for _, pname := range bus.Params.Keys {
			gpio, ok := busParamGPIO(bus.Params.Vals[pname])
			if !ok || !slices.Contains(busSignalParams, pname) {
				continue
			}
			if prev, ok := owners[gpio]; ok {
				h.report("error", "HARDWARE ERROR", "bus-pin-conflict", bus.Region,
					fmt.Sprintf("GPIO %d is used by %s (%s) but is already taken by %s (%s, line %d).",
						gpio, busLabel(bus), pname, prev.bus, prev.role, prev.line),
					"Two buses cannot share a signal pin.",
					sigMovable(bus, pname), regionOf(bus.Params.Vals[pname].(Node)), "Output")
			} else {
				owners[gpio] = owner{busLabel(bus), pname, bus.Region.StartLine}
			}
		}
	}
}

// validateBus checks I2C pins against the legacy ESP32 table (for every chip).
func validateBus(bus *BusDecl, errs *Errors) {
	if bus.BusType != "i2c" {
		return
	}
	for _, sig := range []string{"sda", "scl"} {
		gpio, ok := busParamGPIO(bus.Params.Vals[sig])
		if !ok {
			continue
		}
		if info := legacyPins[gpio]; info != nil && info.Blocked {
			errs.Error(bus.Region, "HARDWARE ERROR",
				fmt.Sprintf("I2C %s pin GPIO %d: %s", strings.ToUpper(sig), gpio, info.BlockedReason), "")
		}
	}
}

func validateDeepSleepPins(program *Program, errs *Errors) {
	if !containsFuncCall(program, "deep_sleep") {
		return
	}
	for _, pin := range program.Hardware.Pins {
		if !strings.HasPrefix(pin.Mode, "Input") {
			continue
		}
		if info := legacyPins[pin.GPIO]; info != nil && !info.CanWakeDeepSleep {
			errs.Warning(pin.Region, "HARDWARE WARNING",
				fmt.Sprintf("GPIO %d cannot wake the ESP32 from deep sleep. Only RTC GPIOs can: 0, 2, 4, 12-15, 25-27, 32-39.", pin.GPIO),
				"If this pin is used as a wake source, use an RTC GPIO instead.")
		}
	}
}

// validateSPISpeed warns when SPI signals go through the GPIO matrix.
func validateSPISpeed(program *Program, errs *Errors) {
	for _, bus := range program.Hardware.Buses {
		if bus.BusType != "spi" {
			continue
		}
		var nonNative []string
		for _, sig := range []string{"mosi", "miso", "clk", "cs"} {
			if gpio, ok := busParamGPIO(bus.Params.Vals[sig]); ok && !slices.Contains(legacyVSPINative, gpio) {
				nonNative = append(nonNative, fmt.Sprintf("%s=GPIO %d", sig, gpio))
			}
		}
		if len(nonNative) > 0 {
			errs.Warning(bus.Region, "HARDWARE WARNING",
				fmt.Sprintf("SPI bus `%s`: pins %s use GPIO matrix routing. Maximum speed limited to 26.6 MHz (full-duplex) / 40 MHz (half-duplex).",
					bus.Name, strings.Join(nonNative, ", ")),
				"For 80 MHz, use VSPI native pins: MOSI=23, MISO=19, CLK=18, CS=5.")
		}
	}
}

// containsMethodCall finds `obj.method()` as a statement or assigned value.
func containsMethodCall(program *Program, obj, method string) bool {
	match := func(e Node) bool {
		// `wifi.connect(...) or retry(5)` uses WiFi as much as the bare call does
		if o, ok := e.(*OrExpr); ok {
			e = o.Expr
		}
		mc, ok := e.(*MethodCall)
		if !ok {
			return false
		}
		v, ok := mc.Obj.(*VarRef)
		return ok && v.Name == obj && mc.Method == method
	}
	for _, s := range collectHardwareStmts(program) {
		switch s := s.(type) {
		case *ExprStmt:
			if match(s.Expr) {
				return true
			}
		case *AssignExpr:
			if match(s.Value) {
				return true
			}
		}
	}
	return false
}

// containsFuncCall finds `name()` as a statement.
func containsFuncCall(program *Program, name string) bool {
	for _, s := range collectHardwareStmts(program) {
		if es, ok := s.(*ExprStmt); ok {
			if fc, ok := es.Expr.(*FuncCall); ok && fc.Name == name {
				return true
			}
		}
	}
	return false
}

// collectHardwareStmts flattens functions, tasks, setup and state
// enter/body blocks (not loop, exit or transitions).
func collectHardwareStmts(program *Program) []Node {
	var out []Node
	for _, f := range program.Functions {
		out = flattenStmts(f.Body, out)
	}
	for _, t := range program.Tasks {
		out = flattenStmts(t.Body, out)
	}
	if program.Setup != nil {
		out = flattenStmts(program.Setup.Body, out)
	}
	for _, m := range program.Machines {
		for _, st := range m.States {
			out = flattenStmts(st.EnterBody, out)
			out = flattenStmts(st.Body, out)
		}
	}
	return out
}

// flattenStmts appends stmts and everything nested in if/while/for/every/
// on/timeout/try blocks (not match).
func flattenStmts(stmts []Node, out []Node) []Node {
	for _, s := range stmts {
		out = append(out, s)
		switch s := s.(type) {
		case *IfStmt:
			out = flattenStmts(s.ThenBody, out)
			switch e := s.ElseBody.(type) {
			case []Node:
				out = flattenStmts(e, out)
			case *IfStmt:
				out = flattenStmts([]Node{e}, out)
			}
		case *WhileStmt:
			out = flattenStmts(s.Body, out)
		case *ForStmt:
			out = flattenStmts(s.Body, out)
		case *EveryBlock:
			out = flattenStmts(s.Body, out)
		case *OnBlock:
			out = flattenStmts(s.Body, out)
		case *TimeoutBlock:
			out = flattenStmts(s.TryBody, out)
			out = flattenStmts(s.FallbackBody, out)
		case *TryBlock:
			// the usual home of wifi.connect
			out = flattenStmts(s.Body, out)
		}
	}
	return out
}
