
chip rp2040
hardware { pin motor: PWM = 5 }
every 1ms strict {
    motor.set_pwm(0)
}
