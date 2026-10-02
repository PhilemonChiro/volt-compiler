
chip esp32

hardware {
    pin motor: PWM = 4
}

var pwm_setpoint = 0

task pid_loop {
    every 1ms strict {
        motor.set_pwm(pwm_setpoint)
    }
}

setup {
    run(pid_loop)
}
