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
	// str(gpio) and the legacy path by int, and the bus check looks up
	// str(gpio) — so it never sees legacy pins. Two maps keep that.
	usedPins := map[string]*PinDecl{}
	legacyUsed := map[int]*PinDecl{}
	usesWifi := containsMethodCall(program, "wifi", "connect")
	for _, pin := range hw.Pins {
		if chip != nil {
			validatePinChip(pin, usedPins, usesWifi, chip, errs)
		} else {
			validatePinLegacy(pin, legacyUsed, usesWifi, errs)
		}
	}

	validateBusPinConflicts(program, usedPins, errs)

	busKinds := map[string]string{}
	for _, b := range hw.Buses {
		busKinds[b.Name] = strings.ToLower(b.BusType)
	}
	usedAddrs := map[string]map[int]*SensorDecl{}
	for _, s := range hw.Sensors {
		validateSensor(s, usedAddrs, busKinds, errs)
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

func validatePinChip(pin *PinDecl, used map[string]*PinDecl, usesWifi bool, chip chipSpec, errs *Errors) {
	name := strconv.Itoa(pin.GPIO)
	issues := chip.validatePin(name, pin.Mode)
	failed := false
	for _, is := range issues {
		if is.severity == "error" {
			errs.Error(pin.Region, "HARDWARE ERROR", is.message, is.hint)
			failed = true
		} else {
			errs.Warning(pin.Region, "HARDWARE WARNING", is.message, is.hint)
		}
	}
	if failed {
		return
	}
	if first, ok := used[name]; ok {
		errs.Error(pin.Region, "HARDWARE ERROR",
			fmt.Sprintf("Pin %s is already used by `%s` (line %d).", name, first.Name, first.Region.StartLine),
			"Each pin can only be used once.")
		return
	}
	used[name] = pin
	if pin.Mode == "Analog" && usesWifi && slices.Contains(chip.adc2GPIOs(), pin.GPIO) {
		errs.Warning(pin.Region, "HARDWARE WARNING",
			fmt.Sprintf("Pin %s is on ADC2, which is blocked while WiFi is active. Analog reads will return errors.", name),
			"Use ADC1 pins instead.")
	}
}

func validatePinLegacy(pin *PinDecl, used map[int]*PinDecl, usesWifi bool, errs *Errors) {
	gpio, r := pin.GPIO, pin.Region
	info := legacyPins[gpio]
	safe := joinInts(legacySafeOutput, "")
	if info == nil {
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d does not exist on ESP32 (valid: 0-39).", gpio), "")
		return
	}
	if info.Blocked {
		errs.Error(r, "HARDWARE ERROR", info.BlockedReason, "Available output pins: "+safe)
		return
	}
	if first, ok := used[gpio]; ok {
		errs.Error(r, "HARDWARE ERROR",
			fmt.Sprintf("GPIO %d is already used by pin `%s` (line %d).", gpio, first.Name, first.Region.StartLine),
			"Each GPIO can only be used once.")
		return
	}
	used[gpio] = pin

	mode := pin.Mode
	switch {
	case mode == "Output" && !info.CanOutput:
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d is input-only — cannot be used as Output.", gpio),
			"Input-only pins: 34-39. Available output pins: "+safe)
		return
	case mode == "PWM" && !info.CanPWM:
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d cannot be used as PWM (input-only pin).", gpio), "")
		return
	case mode == "Analog" && !info.CanAnalog:
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d does not have an ADC channel — cannot be used as Analog.", gpio),
			"ADC1 (safe with WiFi): GPIO 32-39. ADC2 (blocked by WiFi): GPIO 0, 2, 4, 12-15, 25-27.")
		return
	case strings.Contains(mode, "PullUp") && !info.HasInternalPullup:
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d has no internal pull-up resistor (input-only pins 34-39).", gpio),
			"Use an external pull-up resistor, or use a different pin.")
		return
	case strings.Contains(mode, "PullDown") && !info.HasInternalPulldown:
		errs.Error(r, "HARDWARE ERROR", fmt.Sprintf("GPIO %d has no internal pull-down resistor (input-only pins 34-39).", gpio),
			"Use an external pull-down resistor, or use a different pin.")
		return
	}

	if info.IsStrapping && info.StrappingWarning != "" {
		errs.Warning(r, "HARDWARE WARNING", fmt.Sprintf("GPIO %d: %s", gpio, info.StrappingWarning), "")
	}
	if info.IsUART0 {
		errs.Warning(r, "HARDWARE WARNING",
			fmt.Sprintf("GPIO %d is used by UART0 (USB serial). Using it will break serial monitor and firmware flashing.", gpio),
			"Use a different pin unless you specifically intend to disable serial output.")
	}
	if mode == "Analog" && usesWifi && slices.Contains(legacyADC2, gpio) {
		errs.Warning(r, "HARDWARE WARNING",
			fmt.Sprintf("GPIO %d is on ADC2, which is BLOCKED while WiFi is active. Analog reads will return errors or garbage.", gpio),
			"Use ADC1 pins instead (GPIO 32-39) which work with WiFi.")
	}
	if gpio == 36 || gpio == 39 {
		errs.Warning(r, "HARDWARE WARNING",
			fmt.Sprintf("GPIO %d has a hardware errata: interrupts may fire spuriously during WiFi/BT light sleep. Use a different pin for interrupt-driven input.", gpio),
			"GPIO 34, 35 do not have this errata.")
	}
	if info.IsJTAG {
		errs.Warning(r, "HARDWARE WARNING", fmt.Sprintf("GPIO %d is a JTAG pin. Using it disables JTAG debugging.", gpio), "")
	}
	for _, note := range info.Notes {
		errs.Warning(r, "HARDWARE NOTE", fmt.Sprintf("GPIO %d: %s", gpio, note), "")
	}
}

// validateSensor checks I2C address range and per-bus uniqueness. The
// named-pins form (HCSR04(trig: 5, echo: 6)) has no address to check.
func validateSensor(s *SensorDecl, used map[string]map[int]*SensorDecl, busKinds map[string]string, errs *Errors) {
	if len(s.PinParams.Keys) > 0 {
		return
	}
	bus, addr := s.BusName, s.Address
	// 0x00..0x07 and 0x78..0x7F are reserved by the I2C spec.
	if busKinds[bus] == "i2c" && (addr < 0x08 || addr > 0x77) {
		errs.Error(s.Region, "HARDWARE ERROR",
			fmt.Sprintf("I2C address 0x%02X on bus `%s` is outside the 7-bit device range 0x08..0x77.", addr, bus),
			"0x00..0x07 and 0x78..0x7F are reserved by the I²C spec. Check the device datasheet — most parts have a fixed address or an A0/A1 strap option.")
		return
	}
	if used[bus] == nil {
		used[bus] = map[int]*SensorDecl{}
	}
	if first, ok := used[bus][addr]; ok {
		errs.Error(s.Region, "HARDWARE ERROR",
			fmt.Sprintf("I2C address 0x%02X on bus `%s` is already used by sensor `%s` (line %d).", addr, bus, first.Name, first.Region.StartLine),
			"Each I2C address must be unique per bus. Check the sensor datasheet for alternative addresses.")
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
func validateBusPinConflicts(program *Program, usedPins map[string]*PinDecl, errs *Errors) {
	buses := program.Hardware.Buses
	for _, bus := range buses {
		for _, pname := range bus.Params.Keys {
			gpio, ok := busParamGPIO(bus.Params.Vals[pname])
			if !ok || !slices.Contains(busSignalParams, pname) {
				continue
			}
			if first, ok := usedPins[strconv.Itoa(gpio)]; ok {
				errs.Error(bus.Region, "HARDWARE ERROR",
					fmt.Sprintf("GPIO %d is used by bus `%s` (%s) but was already declared as pin `%s` (line %d).",
						gpio, bus.Name, pname, first.Name, first.Region.StartLine),
					"A pin can only belong to one subsystem. Either remove the `pin` declaration or move the bus signal to a free GPIO.")
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
				errs.Error(bus.Region, "HARDWARE ERROR",
					fmt.Sprintf("GPIO %d is used by bus `%s` (%s) but is already taken by bus `%s` (%s, line %d).",
						gpio, bus.Name, pname, prev.bus, prev.role, prev.line),
					"Two buses cannot share a signal pin.")
			} else {
				owners[gpio] = owner{bus.Name, pname, bus.Region.StartLine}
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
// on/timeout blocks (not try or match).
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
		}
	}
	return out
}
