chip esp32
hardware {
    pin led: Output = 4
    pin motor: PWM = 25
}
var counter = 0
task isr_task {
    every 1ms strict {
        counter = counter + 1
    }
}
task slow_task {
    every 1s {
        led.toggle()
        motor.set_pwm(counter)
    }
}
