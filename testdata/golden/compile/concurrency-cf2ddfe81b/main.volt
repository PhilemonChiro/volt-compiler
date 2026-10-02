
chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
}

var read_only_cfg = 100

task critical_reader {
    every 1ms strict {
        motor.set_pwm(read_only_cfg)
    }
}

task background_reader {
    every 500ms {
        led.toggle()
    }
}

setup {
    run(critical_reader, background_reader)
}
