package compiler

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Devices-first hardware: a device names only its own connections and
// settings; the buses it needs are implicit.
//
//	device env:   BME280                      // default I2C bus, default address
//	device oled:  SSD1306 at 0x3D             // `at` = I2C address
//	device rfid:  MFRC522(cs: 5)              // default SPI bus + select pin
//	device gps:   NEO6M(tx: 17, rx: 16)       // own UART port (board-side pins)
//	device env2:  BME280 on sensors at 0x77   // explicit bus
//
// normalizeHardware lowers these onto the legacy shape (every bus device has
// a BusName and Address) so the checks and codegen see one form.

// busDefaultSpeed is the clock codegen uses when a bus sets no speed.
var busDefaultSpeed = map[string]int{"i2c": 400000, "spi": 1000000}

// deviceIface is the device's bus kind from device.json (i2c if unknown).
func deviceIface(dev *DeviceInfo) string {
	if dev == nil {
		return "i2c"
	}
	iface := strings.ToLower(dev.Interface)
	if iface == "serial" {
		return "uart"
	}
	return iface
}

func normalizeHardware(program *Program) {
	hw := program.Hardware
	if hw == nil || hw.normalized {
		return
	}
	hw.normalized = true
	chip := ""
	if program.Chip != nil {
		chip = program.Chip.Target
	}
	native := BusPins(chip)
	taken := map[string]bool{}
	for _, b := range hw.Buses {
		taken[b.Name] = true
	}
	implicit := map[string]*BusDecl{}
	busFor := func(kind string, s *SensorDecl) *BusDecl {
		if b := implicit[kind]; b != nil {
			return b
		}
		name := kind
		if taken[name] {
			name = "default_" + kind
		}
		b := &BusDecl{Base: Base{s.Region}, BusType: kind, Name: name, Implicit: true}
		for _, sig := range []string{"sda", "scl", "mosi", "miso", "clk"} {
			if g, ok := native[kind][sig]; ok {
				b.Params.Set(sig, &IntLiteral{Base{s.Region}, g})
			}
		}
		implicit[kind] = b
		hw.Buses = append(hw.Buses, b)
		return b
	}
	for _, s := range hw.Sensors {
		dev := Registry().Get(s.SensorType)
		iface := deviceIface(dev)
		if s.HasBus {
			if iface == "i2c" && !s.HasAddress && dev != nil {
				s.Address = max(dev.DefaultAddr(), 0)
			}
			continue
		}
		switch iface {
		case "i2c":
			s.BusName = busFor("i2c", s).Name
			if !s.HasAddress && dev != nil {
				s.Address = max(dev.DefaultAddr(), 0)
			}
		case "spi":
			s.BusName = busFor("spi", s).Name
		case "uart":
			// Each UART device gets its own port; tx/rx are the board's pins.
			b := &BusDecl{Base: Base{s.Region}, BusType: "uart", Name: s.Name + "_uart", Implicit: true}
			for _, sig := range []string{"tx", "rx"} {
				if v, ok := s.PinParams.Vals[sig]; ok {
					b.Params.Set(sig, argNode(s, sig, v))
				} else {
					b.Params.Set(sig, &IntLiteral{Base{s.Region}, native["uart"][sig]})
				}
			}
			if v, ok := s.PinParams.Vals["baud"]; ok {
				b.Params.Set("baud", argNode(s, "baud", v))
			} else if p := dev.Param("baud"); p != nil {
				b.Params.Set("baud", &IntLiteral{Base{s.Region}, toInt(p.Default)})
			}
			hw.Buses = append(hw.Buses, b)
			s.BusName = b.Name
		}
	}
	// An implicit bus runs as fast as its slowest device allows.
	for kind, b := range implicit {
		if speed := slowestDevice(hw, b.Name); speed > 0 && speed < busDefaultSpeed[kind] {
			b.Params.Set("speed", &IntLiteral{Base{b.Region}, speed})
		}
	}
}

// slowestDevice is the lowest MaxSpeed among bus's devices (0 = no limit).
func slowestDevice(hw *HardwareBlock, bus string) int {
	low := 0
	for _, s := range hw.Sensors {
		if dev := Registry().Get(s.SensorType); s.BusName == bus && dev != nil && dev.MaxSpeed > 0 {
			if low == 0 || dev.MaxSpeed < low {
				low = dev.MaxSpeed
			}
		}
	}
	return low
}

// argNode turns a parsed named-arg value back into a Node at its source span.
func argNode(s *SensorDecl, key string, v any) Node {
	if n, ok := v.(Node); ok {
		return n
	}
	return &IntLiteral{Base{s.ArgRegions[key]}, toInt(v)}
}

func toInt(v any) int {
	switch v := v.(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

// devicePinKeys are the named args of s that are GPIOs on the device itself
// (not bus signals or settings).
func devicePinKeys(s *SensorDecl) []string {
	dev := Registry().Get(s.SensorType)
	declared := deviceArgNames(dev) != nil
	var keys []string
	for _, k := range s.PinParams.Keys {
		switch {
		case declared && dev.Pin(k) == nil: // a setting, bus signal or unknown arg
		case dev != nil && dev.Param(k) != nil, k == "baud":
		case deviceIface(dev) == "uart" && (k == "tx" || k == "rx"): // bus signals
		default:
			keys = append(keys, k)
		}
	}
	return keys
}

// deviceArgNames lists every named arg device dev accepts, or nil when its
// device.json declares no pins/params (third-party part: anything goes).
func deviceArgNames(dev *DeviceInfo) []string {
	if dev == nil || len(dev.Pins)+len(dev.Params) == 0 && deviceIface(dev) != "uart" {
		return nil
	}
	var names []string
	for _, p := range dev.Pins {
		names = append(names, p.Name)
	}
	if deviceIface(dev) == "uart" {
		names = append(names, "tx", "rx")
	}
	for _, p := range dev.Params {
		names = append(names, p.Name)
	}
	if deviceIface(dev) == "uart" && !slices.Contains(names, "baud") {
		names = append(names, "baud")
	}
	return names
}

// validateDevices checks each device's named args against its device.json,
// runs its own pins through the pin checks (sharing used with `pin`s), and
// checks implicit-bus rules. Bus signal and address checks run elsewhere.
func (h *hwCheck) validateDevices(program *Program, chip chipSpec, usedChip map[string]*PinDecl, usedLegacy map[int]*PinDecl) {
	hw := program.Hardware
	for _, s := range hw.Sensors {
		dev := Registry().Get(s.SensorType)
		if dev == nil {
			continue
		}
		iface := deviceIface(dev)
		if names := deviceArgNames(dev); names != nil {
			for _, k := range s.PinParams.Keys {
				if !slices.Contains(names, k) {
					h.errs.add(Diagnostic{Region: argRegion(s, k), Category: "DEVICE ERROR", Severity: "error", Code: "device-unknown-arg",
						Message: fmt.Sprintf("`%s` has no setting `%s`.", s.SensorType, k),
						Hint:    fmt.Sprintf("%s accepts: %s.", s.SensorType, strings.Join(names, ", "))})
				}
			}
		}
		for _, p := range dev.Params {
			v, ok := s.PinParams.Vals[p.Name]
			if ok && !paramTypeOK(p.Type, v) {
				h.errs.add(Diagnostic{Region: argRegion(s, p.Name), Category: "DEVICE ERROR", Severity: "error", Code: "device-arg-type",
					Message: fmt.Sprintf("`%s` of `%s` must be %s %s.", p.Name, s.SensorType, article(p.Type), p.Type),
					Hint:    fmt.Sprintf("Default: %s: %v", p.Name, p.Default)})
			}
		}
		atMisused := s.HasAddress && !s.HasBus && iface != "i2c"
		if atMisused {
			d := Diagnostic{Region: s.AddressRegion, Category: "DEVICE ERROR", Severity: "error", Code: "device-at-not-i2c",
				Message: fmt.Sprintf("`at` sets an I2C address, but %s is a %s device.", s.SensorType, strings.ToUpper(iface)), Hint: "Remove `at`."}
			if cs := dev.Pin("cs"); cs != nil && len(dev.Pins) == 1 && len(s.PinParams.Keys) == 0 {
				d.Hint = fmt.Sprintf("Name the select pin instead: `device %s: %s(cs: %d)`", s.Name, s.SensorType, s.Address)
				d.Fixes = []Fix{{Title: fmt.Sprintf("Use cs: %d", s.Address),
					Region: Region{s.TypeRegion.EndLine, s.TypeRegion.EndCol, s.AddressRegion.EndLine, s.AddressRegion.EndCol},
					Text:   fmt.Sprintf("(cs: %d)", s.Address)}}
			}
			h.errs.add(d)
		}
		// Required pins. Legacy `on bus at N` / `on gpio at N` supplies a
		// single pin through the address.
		var missing []string
		for _, p := range dev.Pins {
			if _, ok := s.PinParams.Vals[p.Name]; !ok && !(s.HasAddress && s.HasBus && len(dev.Pins) == 1) {
				missing = append(missing, p.Name)
			}
		}
		if len(missing) > 0 && !atMisused {
			args := make([]string, len(dev.Pins))
			for i, p := range dev.Pins {
				args[i] = p.Name + ": <gpio>"
			}
			h.errs.add(Diagnostic{Region: s.Region, Category: "DEVICE PIN ERROR", Severity: "error", Code: "device-missing-pin",
				Message: fmt.Sprintf("`%s` needs pin%s %s. Missing: %s.", s.SensorType, plural(len(dev.Pins)), joinNames(dev.Pins), strings.Join(missing, ", ")),
				Hint:    fmt.Sprintf("`device %s: %s(%s)`", s.Name, s.SensorType, strings.Join(args, ", "))})
		}
		// The device's own pins go through the same checks as `pin`s.
		for _, k := range devicePinKeys(s) {
			g, ok := s.PinParams.Vals[k].(int)
			if !ok {
				continue
			}
			mode := "Output"
			if p := dev.Pin(k); p != nil && p.Dir == "in" {
				mode = "Input"
			}
			r := argRegion(s, k)
			pin := &PinDecl{Base: Base{r}, Name: s.Name + "." + k, Mode: mode, GPIO: g, GPIORegion: r}
			if chip != nil {
				h.validatePinChip(pin, usedChip, chip)
			} else {
				h.validatePinLegacy(pin, usedLegacy)
			}
		}
	}
	h.validateUARTCount(program, chip)
	h.validateBusSpeeds(hw)
}

// validateUARTCount: ESP32 and RP2040 have two UARTs free for devices
// (ESP32's UART0 is the USB console).
func (h *hwCheck) validateUARTCount(program *Program, chip chipSpec) {
	limit, label := 2, "ESP32 has 2 UARTs free for devices (UART1, UART2; UART0 is the USB console)"
	if chip != nil {
		switch chip.(type) {
		case esp32Chip:
		case rp2040Chip:
			label = "RP2040 has 2 UARTs (UART0, UART1)"
		default:
			return
		}
	}
	n := 0
	for _, b := range program.Hardware.Buses {
		if b.BusType != "uart" && b.BusType != "serial" {
			continue
		}
		if n++; n > limit {
			who := fmt.Sprintf("bus `%s`", b.Name)
			if b.Implicit {
				who = fmt.Sprintf("`%s`", strings.TrimSuffix(b.Name, "_uart"))
			}
			h.errs.add(Diagnostic{Region: b.Region, Category: "HARDWARE ERROR", Severity: "error", Code: "uart-exhausted",
				Message: fmt.Sprintf("No UART left for %s: %s.", who, label),
				Hint:    "Remove a serial device, or use an I2C/SPI variant of the part."})
		}
	}
}

// validateBusSpeeds warns when an explicit bus clock is faster than a device
// on it supports.
func (h *hwCheck) validateBusSpeeds(hw *HardwareBlock) {
	for _, b := range hw.Buses {
		lit, ok := b.Params.Vals["speed"].(*IntLiteral)
		if b.Implicit || !ok {
			continue
		}
		for _, s := range hw.Sensors {
			dev := Registry().Get(s.SensorType)
			if s.BusName != b.Name || dev == nil || dev.MaxSpeed == 0 || lit.Value <= dev.MaxSpeed {
				continue
			}
			h.errs.add(Diagnostic{Region: lit.Region, Category: "HARDWARE WARNING", Severity: "warning", Code: "bus-too-fast",
				Message: fmt.Sprintf("Bus `%s` runs at %s but `%s` (%s) supports at most %s.", b.Name, hz(lit.Value), s.Name, s.SensorType, hz(dev.MaxSpeed)),
				Fixes:   []Fix{{Title: "Use " + hz(dev.MaxSpeed), Region: lit.Region, Text: freqLiteral(dev.MaxSpeed)}}})
			break
		}
	}
}

// validateAddressList warns when an I2C address is not one the part can
// answer at, offering the ones it can.
func validateAddressList(s *SensorDecl, errs *Errors) {
	dev := Registry().Get(s.SensorType)
	if dev == nil || !s.HasAddress || len(dev.Addresses) == 0 || s.Address < 0x08 || s.Address > 0x77 {
		return
	}
	var fixes []Fix
	for _, a := range dev.Addresses {
		v, ok := parseAddr(a)
		if !ok {
			continue
		}
		if v == s.Address {
			return
		}
		if len(fixes) < 3 {
			fixes = append(fixes, Fix{Title: fmt.Sprintf("Use 0x%02X", v), Region: s.AddressRegion, Text: fmt.Sprintf("0x%02X", v)})
		}
	}
	errs.add(Diagnostic{Region: s.AddressRegion, Category: "HARDWARE WARNING", Severity: "warning", Code: "i2c-address-unsupported",
		Message: fmt.Sprintf("%s does not answer at 0x%02X. It can use: %s.", s.SensorType, s.Address, strings.Join(dev.Addresses, ", ")),
		Fixes:   fixes})
}

func argRegion(s *SensorDecl, k string) Region {
	if r, ok := s.ArgRegions[k]; ok {
		return r
	}
	return s.Region
}

func paramTypeOK(typ string, v any) bool {
	switch v.(type) {
	case int:
		return typ == "int" || typ == "float"
	case *FloatLiteral:
		return typ == "float"
	case *StringLiteral:
		return typ == "string"
	case *BoolLiteral:
		return typ == "bool"
	}
	return true // an expression: the C compiler has the last word
}

func article(typ string) string {
	if typ == "int" {
		return "an"
	}
	return "a"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinNames(pins []DevicePin) string {
	names := make([]string, len(pins))
	for i, p := range pins {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

func hz(v int) string {
	switch {
	case v%1000000 == 0:
		return strconv.Itoa(v/1000000) + " MHz"
	case v%1000 == 0:
		return strconv.Itoa(v/1000) + " kHz"
	}
	return strconv.Itoa(v) + " Hz"
}

func freqLiteral(v int) string {
	switch {
	case v%1000000 == 0:
		return strconv.Itoa(v/1000000) + "M"
	case v%1000 == 0:
		return strconv.Itoa(v/1000) + "k"
	}
	return strconv.Itoa(v)
}

// paramValue is a device param's C text: as written, else its default.
func paramValue(s *SensorDecl, p *DeviceParam) string {
	if v, ok := s.PinParams.Vals[p.Name]; ok {
		return pyStr(v)
	}
	switch d := p.Default.(type) {
	case float64:
		if p.Type == "int" || d == float64(int(d)) {
			return strconv.Itoa(int(d))
		}
		return strconv.FormatFloat(d, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(d)
	case string:
		return strconv.Quote(d)
	case nil:
		return "0"
	}
	return fmt.Sprint(p.Default)
}

// busLabel names a bus in messages; implicit buses have no name in source.
func busLabel(b *BusDecl) string {
	switch {
	case !b.Implicit:
		return fmt.Sprintf("bus `%s`", b.Name)
	case b.BusType == "uart":
		return fmt.Sprintf("`%s`'s UART", strings.TrimSuffix(b.Name, "_uart"))
	}
	return "the default " + strings.ToUpper(b.BusType) + " bus"
}

// validateBusPinCaps: each bus signal must exist and be able to drive (or,
// for rx/miso, read) the line. Blocked I2C pins are reported by validateBus.
func (h *hwCheck) validateBusPinCaps(program *Program, chip chipSpec) {
	for _, b := range program.Hardware.Buses {
		for _, sig := range b.Params.Keys {
			g, ok := busParamGPIO(b.Params.Vals[sig])
			if !ok || !slices.Contains(busSignalParams, sig) {
				continue
			}
			mode := "Output"
			if sig == "rx" || sig == "miso" {
				mode = "Input"
			}
			r := regionOf(b.Params.Vals[sig].(Node))
			report := func(code, msg, hint string) {
				h.report("error", "HARDWARE ERROR", code, r,
					fmt.Sprintf("%s (%s): %s", busLabel(b), sig, msg), hint, sigMovable(b, sig), r, mode)
			}
			if chip == nil {
				switch info := legacyPins[g]; {
				case info == nil:
					report("pin-unknown", fmt.Sprintf("GPIO %d does not exist on ESP32.", g), "")
				case mode == "Output" && !info.CanOutput && !info.Blocked:
					report("pin-input-only", fmt.Sprintf("GPIO %d is input-only.", g), "")
				}
				continue
			}
			for _, is := range chip.validatePin(strconv.Itoa(g), mode) {
				if is.severity == "error" && (is.code != "pin-blocked" || b.BusType != "i2c") {
					report(is.code, is.message, is.hint)
				}
			}
		}
	}
}

// sigMovable: a bus signal's number is in the source (so a quick fix can
// move it), unless it is an implicit bus's chip-native default.
func sigMovable(b *BusDecl, sig string) bool {
	return !b.Implicit || regionOf(b.Params.Vals[sig].(Node)) != b.Region
}
