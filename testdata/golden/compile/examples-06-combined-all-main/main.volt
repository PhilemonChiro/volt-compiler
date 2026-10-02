/* Combined: functions, state machines, tasks, storage, ADC */

hardware {
    pin led = 2
    pin motor: PWM = 5
    pin battery: Analog = 36
}

func clamp(val: int, lo: int, hi: int) -> int {
    if val < lo { return lo }
    if val > hi { return hi }
    return val
}

func scale(raw: int) -> int {
    return clamp(raw * 100 / 4095, 0, 100)
}

machine power_manager {
    var battery_pct: int = 100

    state normal {
        enter {
            log("power", "normal")
            led.on()
        }
        exit {
            led.off()
        }
        after 10s -> check_battery
    }

    state check_battery {
        enter {
            battery_pct = scale(battery.read())
            log("power", "battery check")
        }
        after 100ms -> normal
    }

    state low_power {
        enter {
            log("power", "LOW")
        }
        after 30s -> check_battery
    }
}

every 100ms { led.toggle() }

every 1ms strict {
    motor.set_pwm(0)
}

task slow_counter {
    var n: int = 0
    every 2s {
        n = n + 1
        let bat: int = battery.read()
        let pct: int = scale(bat)
        log("status", "tick")
    }
}
