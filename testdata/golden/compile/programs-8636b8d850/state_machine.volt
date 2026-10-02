chip esp32

hardware {
    pin led: Output = 2
    pin buzzer: PWM = 18
    pin button: PullUpInput = 4
}

machine Alarm {
    let attempts: int = 0

    initial state idle {
        enter {
            led.off()
            buzzer.set_pwm(0)
            log("alarm", "STATE -> idle")
        }
        on arm_cmd -> armed
    }

    state armed {
        enter {
            led.on()
            log("alarm", "STATE -> armed | waiting for motion")
        }
        on motion -> triggered
        on disarm_cmd -> idle
    }

    state triggered {
        enter {
            buzzer.set_pwm(200)
            log("alarm", "STATE -> triggered | buzzer=200 attempts=" + attempts)
        }
        exit {
            buzzer.set_pwm(0)
        }
        on correct_code -> idle
        on wrong_code -> triggered
        after 60s -> siren
    }

    state siren {
        enter {
            buzzer.set_pwm(255)
            log("alarm", "STATE -> siren | full alarm")
        }
        exit {
            buzzer.set_pwm(0)
        }
        after 300s -> idle
        on master_code -> idle
    }
}

setup {
}
