package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

// Port of the parts of volt/chips (esp32, esp8266, rp2040) that hardware
// validation uses. Pin databases are generated into chips_tables.go.

// pinIssue is one finding from chipSpec.validatePin; hint "" means none.
type pinIssue struct{ severity, message, hint string }

type chipSpec interface {
	displayName() string
	supports(feature string) bool
	validatePin(pinName, mode string) []pinIssue
	adc2GPIOs() []int
}

// getChip looks up a chip by name or alias, ignoring case, '-' and '_'.
func getChip(name string) chipSpec {
	key := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	switch key {
	case "esp32", "esp32wroom":
		return esp32Chip{}
	case "esp8266", "esp8266ex", "esp12":
		return esp8266Chip{}
	case "rp2040", "pico", "raspberrypi", "rpipico":
		return rp2040Chip{}
	}
	return nil
}

func joinInts(xs []int, prefix string) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = prefix + strconv.Itoa(x)
	}
	return strings.Join(s, ", ")
}

// pinByNumber is ESP get_pin: the name must parse as an int.
func pinByNumber(db map[int]*pinInfo, name string) *pinInfo {
	n, err := strconv.Atoi(name)
	if err != nil {
		return nil
	}
	return db[n]
}

func uartDir(p *pinInfo) string {
	if p.GPIO == 1 {
		return "TX"
	}
	return "RX"
}

// ---- ESP32 ----

type esp32Chip struct{}

func (esp32Chip) displayName() string { return "ESP32 (WROOM-32)" }
func (esp32Chip) adc2GPIOs() []int    { return esp32ADC2 }

func (esp32Chip) supports(f string) bool {
	switch f {
	case "wifi", "bluetooth", "ble", "pwm", "dac", "touch", "rtos", "hardware_timer", "deep_sleep", "adc":
		return true
	}
	return false
}

func (esp32Chip) validatePin(name, mode string) []pinIssue {
	pin := pinByNumber(esp32Pins, name)
	if pin == nil {
		return []pinIssue{{"error", fmt.Sprintf("GPIO %s does not exist on ESP32.", name),
			"Available pins: 0-39 (excluding 20, 24, 28-31)"}}
	}
	if pin.Blocked {
		return []pinIssue{{"error", pin.BlockedReason, ""}}
	}
	var out []pinIssue
	safe := "Available output pins: " + joinInts(esp32SafeOutput, "")
	if pin.IsFlashPin {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s is connected to SPI flash — using it will crash the chip.", name), safe})
	}
	if mode == "Output" && !pin.CanOutput {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s is input-only (GPIO 34-39 cannot output).", name), safe})
	}
	if mode == "Analog" && !pin.CanAnalog {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s has no ADC channel.", name),
			"Pins with ADC: " + joinInts(esp32ADCPins, "")})
	}
	if (mode == "PullUpInput" || mode == "InputPullUp") && !pin.HasInternalPullup {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s has no internal pull-up resistor (GPIO 34-39).", name),
			"Use an external pull-up resistor."})
	}
	if pin.IsStrapping && pin.StrappingWarning != "" {
		out = append(out, pinIssue{"warning", "Strapping pin: " + pin.StrappingWarning, ""})
	}
	if pin.IsUART0 {
		out = append(out, pinIssue{"warning", fmt.Sprintf("GPIO %s is UART0 (%s) — using it may break serial communication.", name, uartDir(pin)), ""})
	}
	return out
}

// ---- ESP8266 ----

type esp8266Chip struct{}

func (esp8266Chip) displayName() string { return "ESP8266 (ESP-12E/F)" }
func (esp8266Chip) adc2GPIOs() []int    { return nil }

func (esp8266Chip) supports(f string) bool {
	switch f {
	case "wifi", "pwm", "rtos", "hardware_timer", "deep_sleep", "adc":
		return true
	}
	return false
}

func (esp8266Chip) validatePin(name, mode string) []pinIssue {
	safe := "Available output pins: " + joinInts(esp8266SafeOutput, "")
	pin := pinByNumber(esp8266Pins, name)
	if pin == nil {
		return []pinIssue{{"error", fmt.Sprintf("GPIO %s does not exist on ESP8266. ESP8266 has GPIO 0-16 only.", name), safe}}
	}
	if pin.Blocked {
		return []pinIssue{{"error", pin.BlockedReason, ""}}
	}
	var out []pinIssue
	if pin.IsFlashPin {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s is connected to SPI flash — using it will crash.", name), safe})
	}
	if mode == "Output" && !pin.CanOutput {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s cannot be used as output.", name), safe})
	}
	if mode == "Analog" {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s is not an analog pin. ESP8266 has only one ADC input (A0/TOUT) which is not a GPIO.", name),
			"Use the dedicated A0 pin for analog reads."})
	}
	if mode == "Touch" {
		out = append(out, pinIssue{"error", "ESP8266 does not have capacitive touch pins.", "Capacitive touch is available on ESP32."})
	}
	if (mode == "PullDownInput" || mode == "InputPullDown") && pin.GPIO != 16 {
		out = append(out, pinIssue{"error", "ESP8266 GPIOs 0-15 do not support pull-down. Only GPIO 16 (RTC domain) has internal pull-down.",
			"Use an external pull-down resistor."})
	}
	if (mode == "PullUpInput" || mode == "InputPullUp") && !pin.HasInternalPullup {
		out = append(out, pinIssue{"error", fmt.Sprintf("GPIO %s has no internal pull-up (GPIO 16 is RTC domain).", name),
			"Use an external pull-up resistor."})
	}
	if pin.GPIO == 16 && mode == "PWM" {
		out = append(out, pinIssue{"error", "GPIO 16 does not support PWM (RTC domain, different hardware).",
			"PWM-capable pins: " + joinInts(esp8266SafeOutput, "")})
	}
	if pin.IsStrapping && pin.StrappingWarning != "" {
		out = append(out, pinIssue{"warning", "Strapping pin: " + pin.StrappingWarning, ""})
	}
	if pin.IsUART0 {
		out = append(out, pinIssue{"warning", fmt.Sprintf("GPIO %s is UART0 (%s) — using it will break serial communication.", name, uartDir(pin)), ""})
	}
	return out
}

// ---- RP2040 ----

type rp2040Chip struct{}

func (rp2040Chip) displayName() string { return "RP2040 (Raspberry Pi Pico)" }
func (rp2040Chip) adc2GPIOs() []int    { return nil }

func (rp2040Chip) supports(f string) bool {
	switch f {
	case "pwm", "hardware_timer", "deep_sleep", "adc", "usb", "pio":
		return true
	}
	return false
}

func (rp2040Chip) getPin(name string) *pinInfo {
	if up := strings.ToUpper(name); strings.HasPrefix(up, "GP") {
		return pinByNumber(rp2040Pins, up[2:])
	}
	return pinByNumber(rp2040Pins, name)
}

func (c rp2040Chip) validatePin(name, mode string) []pinIssue {
	pin := c.getPin(name)
	if pin == nil {
		return []pinIssue{{"error", fmt.Sprintf("Pin %s does not exist on RP2040. RP2040 has GP0-GP28.", name),
			"Available pins: GP0-GP22, GP25-GP28"}}
	}
	if pin.Blocked {
		return []pinIssue{{"error", pin.BlockedReason, ""}}
	}
	var out []pinIssue
	if mode == "Output" && !pin.CanOutput {
		out = append(out, pinIssue{"error", pin.Name + " cannot be used as output.",
			"Available output pins: " + joinInts(rp2040SafeOutput, "GP")})
	}
	if mode == "Analog" && !pin.CanAnalog {
		out = append(out, pinIssue{"error", pin.Name + " is not an analog pin. RP2040 ADC is on GP26, GP27, GP28 only.",
			"Analog pins: " + joinInts(rp2040ADCPins, "GP")})
	}
	if mode == "Touch" {
		out = append(out, pinIssue{"error", "RP2040 does not have capacitive touch pins.", "Capacitive touch is available on ESP32."})
	}
	if pin.GPIO == 25 {
		out = append(out, pinIssue{"warning", "GP25 is the onboard LED on Pico. On Pico W, GP25 is used for WiFi SPI — not available as GPIO.", ""})
	}
	if pin.GPIO == 23 || pin.GPIO == 24 {
		out = append(out, pinIssue{"warning", pin.Name + " is used by the Pico board's power circuit. Using it may affect power management.", ""})
	}
	return out
}
