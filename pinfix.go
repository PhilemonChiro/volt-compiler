package compiler

import (
	"slices"
	"strconv"
	"strings"
)

// Quick-fix suggestions for hardware diagnostics: free GPIOs that can do
// what a pin (or bus signal) needs, safest first.

// pinCaps is what pin suggestions need to know about a chip.
type pinCaps struct {
	table      map[int]*pinInfo
	dac, touch []int // nil: chip has none
	adc2       []int // ADC2 GPIOs (unusable for Analog while WiFi is on)
}

func (esp32Chip) caps() pinCaps {
	return pinCaps{esp32Pins, esp32DACPins, esp32TouchPins, esp32ADC2}
}
func (esp8266Chip) caps() pinCaps { return pinCaps{table: esp8266Pins} }
func (rp2040Chip) caps() pinCaps  { return pinCaps{table: rp2040Pins} }

var legacyCaps = pinCaps{legacyPins, esp32DACPins, esp32TouchPins, legacyADC2}

// takenGPIOs collects every GPIO the hardware block claims: pins, bus
// signals and named device pins.
func takenGPIOs(hw *HardwareBlock) map[int]bool {
	taken := map[int]bool{}
	for _, p := range hw.Pins {
		taken[p.GPIO] = true
	}
	for _, b := range hw.Buses {
		for _, k := range b.Params.Keys {
			if g, ok := busParamGPIO(b.Params.Vals[k]); ok && slices.Contains(busSignalParams, k) {
				taken[g] = true
			}
		}
	}
	for _, s := range hw.Sensors {
		for _, k := range devicePinKeys(s) {
			if g, ok := s.PinParams.Vals[k].(int); ok {
				taken[g] = true
			}
		}
	}
	return taken
}

// supportsMode reports whether a pin can serve a mode ("" = inferred,
// treated as Output).
func (c pinCaps) supportsMode(p *pinInfo, mode string, usesWifi bool) bool {
	switch mode {
	case "", "Output":
		return p.CanOutput
	case "PWM", "Servo":
		return p.CanOutput && p.CanPWM
	case "DAC":
		return p.CanOutput && slices.Contains(c.dac, p.GPIO)
	case "Analog":
		return p.CanAnalog && !(usesWifi && slices.Contains(c.adc2, p.GPIO))
	case "Touch":
		return slices.Contains(c.touch, p.GPIO)
	case "PullUpInput", "InputPullUp":
		return p.HasInternalPullup
	case "PullDownInput", "InputPullDown":
		return p.HasInternalPulldown
	}
	return true // Input and anything unknown
}

// suggestPins returns up to n free GPIOs for mode: never blocked, flash,
// strapping or UART0 pins; clean pins before JTAG/annotated ones; for
// input modes, input-only pins first so output-capable pins stay free.
func (c pinCaps) suggestPins(mode string, taken map[int]bool, usesWifi bool, n int) []int {
	inputish := mode == "Input" || mode == "Analog"
	var tiers [3][]int
	for g, p := range c.table {
		if taken[g] || p.Blocked || p.IsFlashPin || p.IsStrapping || p.IsUART0 || !c.supportsMode(p, mode, usesWifi) {
			continue
		}
		t := 1
		switch {
		case p.IsJTAG || slices.ContainsFunc(p.Notes, func(n string) bool { return noteCatch(n) != "" }):
			t = 2
		case inputish && !p.CanOutput:
			t = 0
		}
		tiers[t] = append(tiers[t], g)
	}
	var out []int
	for _, t := range tiers {
		slices.Sort(t)
		out = append(out, t...)
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// moveFixes offers "Move to GPIO N" edits on the number at r.
func moveFixes(r Region, gpios []int) []Fix {
	if r == (Region{}) {
		return nil
	}
	fixes := make([]Fix, len(gpios))
	for i, g := range gpios {
		s := strconv.Itoa(g)
		fixes[i] = Fix{Title: "Move to GPIO " + s, Region: r, Text: s}
	}
	return fixes
}

// GPIOInfo describes one GPIO for editor tooling (completion, hover, the
// board view). Safety: "safe"; "caution" (usable, with a catch: JTAG,
// errata, module notes); "risky" (strapping / UART0 — never suggested);
// "unavailable" (flash or not bonded out).
type GPIOInfo struct {
	GPIO   int      `json:"gpio"`
	Caps   []string `json:"caps"` // output pwm analog adc2 dac touch pullup pulldown
	Safety string   `json:"safety"`
	Reason string   `json:"reason"` // short tag for the safety level, "" when safe
	Notes  []string `json:"notes"`  // full explanations
}

// PinTable lists a chip's GPIOs in ascending order; an unknown or empty
// chip gets the legacy ESP32 table (what validation uses then too).
func PinTable(chip string) []GPIOInfo {
	c := legacyCaps
	if spec := getChip(chip); spec != nil {
		c = spec.caps()
	}
	out := []GPIOInfo{}
	for g, p := range c.table {
		info := GPIOInfo{GPIO: g, Caps: []string{}, Safety: "safe", Notes: []string{}}
		for _, cp := range []struct {
			name string
			ok   bool
		}{
			{"output", p.CanOutput}, {"pwm", p.CanOutput && p.CanPWM}, {"analog", p.CanAnalog},
			{"adc2", p.CanAnalog && slices.Contains(c.adc2, g)}, {"dac", slices.Contains(c.dac, g)},
			{"touch", slices.Contains(c.touch, g)}, {"pullup", p.HasInternalPullup}, {"pulldown", p.HasInternalPulldown},
		} {
			if cp.ok {
				info.Caps = append(info.Caps, cp.name)
			}
		}
		set := func(level, reason, note string) {
			if info.Reason == "" {
				info.Safety, info.Reason = level, reason
			}
			if note != "" {
				info.Notes = append(info.Notes, note)
			}
		}
		switch {
		case p.Blocked || p.IsFlashPin:
			set("unavailable", "unavailable", p.BlockedReason)
		case p.IsStrapping && g == 2 && !strings.Contains(strings.ToLower(chip), "8266"):
			set("caution", "on-board LED", "Drives the on-board LED. Fine for outputs; don't wire a button or sensor that pulls it HIGH at power-up.")
		case p.IsStrapping:
			set("risky", "strapping", p.StrappingWarning)
		case p.IsUART0:
			set("risky", "UART0 "+uartDir(p), "")
		}
		if p.IsJTAG {
			set("caution", "JTAG", "JTAG pin: using it disables JTAG debugging.")
		}
		for _, n := range p.Notes {
			if c := noteCatch(n); c != "" {
				set("caution", c, n)
			} else {
				info.Notes = append(info.Notes, n)
			}
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b GPIOInfo) int { return a.GPIO - b.GPIO })
	return out
}

// noteCatch tags a pin note that makes the pin a second choice; notes
// that are only informational (DAC channel, 32 kHz crystal) give "".
func noteCatch(n string) string {
	switch {
	case strings.Contains(n, "PSRAM"):
		return "PSRAM"
	case strings.Contains(n, "errata"):
		return "errata"
	case strings.Contains(n, "Not exposed"):
		return "not on DevKit"
	}
	return ""
}

// BusPins are the pins to suggest for a new bus, by bus type and signal: the
// chip's native peripheral pins. ESP32 suggests UART2 (17/16) because UART0
// (1/3) is the USB console.
func BusPins(chip string) map[string]map[string]int {
	t := strings.ToLower(chip)
	switch {
	case strings.Contains(t, "rp2040") || strings.Contains(t, "pico"):
		return map[string]map[string]int{
			"i2c":  {"sda": 4, "scl": 5},
			"spi":  {"mosi": 19, "miso": 16, "clk": 18},
			"uart": {"tx": 8, "rx": 9}, // UART1
		}
	case strings.Contains(t, "8266"):
		return map[string]map[string]int{
			"i2c":  {"sda": 4, "scl": 5},
			"spi":  {"mosi": 13, "miso": 12, "clk": 14},
			"uart": {"tx": 15, "rx": 13},
		}
	}
	return map[string]map[string]int{
		"i2c":  {"sda": 21, "scl": 22},
		"spi":  {"mosi": 23, "miso": 19, "clk": 18},
		"uart": {"tx": 17, "rx": 16},
	}
}
