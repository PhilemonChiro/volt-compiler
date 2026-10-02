
chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
}

var shared_counter = 0

task fast_control {
    every 1ms strict {
        motor.set_pwm(shared_counter)
    }
}

task slow_monitor {
    every 500ms {
        shared_counter = shared_counter + 1
        led.toggle()
    }
}

setup {
    run(fast_control, slow_monitor)
}
