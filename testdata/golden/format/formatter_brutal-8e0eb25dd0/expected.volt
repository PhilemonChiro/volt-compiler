/* ============================================================
Smart LED Controller — Raspberry Pi Pico

A multi-mode LED controller with button cycling.
Demonstrates: tasks, state machines, PWM, channels,
shared vars, string interpolation.

Hardware:
GP25 = onboard LED (PWM breathing)
GP15 = button (cycle modes)
GP0  = status indicator
============================================================ */

hardware {
    pin led: PWM = GP25
    pin button: PullUpInput = GP15
    pin status: Output = GP0
}

setup {
    log("boot", "Smart LED Controller")
    log("boot", "modes: 0=breathe 1=blink 2=heartbeat 3=off")
    log("boot", "press button on GP15 to cycle")
}

/* Mode tracking — auto-detected as shared across tasks */
var current_mode: int = 0
var mode_count: int = 4

/* Button events flow through a channel */
channel button_events: int

/* ---- Button handler: debounced press detection ---- */
task button_watcher {
    var last_state: int = 1
    var press_count: int = 0

    every 20ms {
        let state = button.read()
        if state == 0 {
            if last_state == 1 {
                press_count += 1
                button_events.send(press_count)
            }
        }
        last_state = state
    }
}

/* ---- Mode controller: processes button events ---- */
task mode_controller {
    on button_events.has_data() {
        let event = button_events.receive()
        current_mode = (current_mode + 1) % mode_count
        log("mode", "switched to mode {current_mode}")
        status.toggle()
    }
}

/* ---- LED driver: runs the current pattern ---- */
task led_driver {
    var brightness: int = 0
    var direction: int = 1
    var blink_state: int = 0
    var pulse_phase: int = 0

    every 10ms {
        if current_mode == 0 {
            /* Mode 0: Breathing — smooth fade in/out */
            brightness += direction * 3
            if brightness >= 255 {
                brightness = 255
                direction = -1
            }
            if brightness <= 0 {
                brightness = 0
                direction = 1
            }
            led.set_pwm(brightness)
        }

        if current_mode == 1 {
            /* Mode 1: Fast blink */
            blink_state += 1
            if blink_state >= 10 {
                blink_state = 0
                if brightness == 0 {
                    brightness = 255
                } else {
                    brightness = 0
                }
                led.set_pwm(brightness)
            }
        }

        if current_mode == 2 {
            /* Mode 2: Slow pulse — heartbeat pattern */
            pulse_phase += 1
            if pulse_phase >= 200 {
                pulse_phase = 0
            }
            if pulse_phase < 20 {
                led.set_pwm(200)
            }
            if pulse_phase >= 20 {
                if pulse_phase < 40 {
                    led.set_pwm(0)
                }
            }
            if pulse_phase >= 40 {
                if pulse_phase < 60 {
                    led.set_pwm(150)
                }
            }
            if pulse_phase >= 60 {
                led.set_pwm(0)
            }
        }

        if current_mode == 3 {
            /* Mode 3: Off */
            led.set_pwm(0)
        }
    }
}

/* ---- Health reporter ---- */
task health {
    every 5s {
        log("health", "mode={current_mode} uptime={system.uptime()}s heap={system.free_heap()}")
    }
}

/* ---- Startup ---- */
