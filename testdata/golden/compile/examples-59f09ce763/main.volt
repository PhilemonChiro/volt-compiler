/* Stress test: 5-phase sequencer + watchdog + multiple tasks */

hardware {
    pin led = 2
    pin motor: PWM = 4
}

setup {
    system.watchdog_enable(15000)
}

machine sequencer {
    var step: int = 0
    var total_cycles: int = 0

    state phase_a {
        enter {
            step = 0
            led.on()
            log("seq", "phase A")
        }
        exit {
            led.off()
        }
        after 1s -> phase_b
    }

    state phase_b {
        enter {
            step = step + 1
            log("seq", "phase B")
        }
        after 800ms -> phase_c
    }

    state phase_c {
        enter {
            step = step + 1
            log("seq", "phase C")
        }
        after 600ms -> phase_d
    }

    state phase_d {
        enter {
            step = step + 1
            log("seq", "phase D")
        }
        after 400ms -> phase_e
    }

    state phase_e {
        enter {
            total_cycles = total_cycles + 1
            log("seq", "cycle complete")
        }
        after 200ms -> phase_a
    }
}

every 1ms strict {
    motor.set_pwm(0)
}

every 500ms {
    system.watchdog_feed()
    led.toggle()
}
