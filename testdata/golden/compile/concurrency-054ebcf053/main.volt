
chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
}

var mixed_val = 0
var private_a = 10
var private_b = 20

task isr_task {
    every 1ms strict {
        motor.set_pwm(mixed_val)
        private_a = private_a + 1
    }
}

task bg_task {
    every 500ms {
        mixed_val = mixed_val + 1
        private_b = private_b + 1
        led.toggle()
    }
}

setup {
    run(isr_task, bg_task)
}
