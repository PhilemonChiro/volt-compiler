
hardware {
    pin motor: PWM = 4
}
task pid {
    every 1ms strict {
        motor.set_pwm(0)
    }
}
