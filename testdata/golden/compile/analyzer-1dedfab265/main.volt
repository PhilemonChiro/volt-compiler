
hardware {
    pin motor: PWM = 4
}
var shared_val = 0

task reader {
    every 1second {
        log("val")
    }
}

task writer {
    every 1ms strict {
        motor.set_pwm(shared_val)
    }
}
