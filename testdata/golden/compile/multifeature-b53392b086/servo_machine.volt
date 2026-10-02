
chip esp32
hardware {
    pin servo_pin: PWM = 13
}

machine arm {
    state home {
        enter {
            servo_pin.set_pwm(0)
        }
        after 3s -> sweep_right
    }
    state sweep_right {
        enter {
            servo_pin.set_pwm(255)
        }
        after 2s -> sweep_left
    }
    state sweep_left {
        enter {
            servo_pin.set_pwm(0)
        }
        after 2s -> home
    }
}
