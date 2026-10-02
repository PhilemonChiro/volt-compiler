
chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
    pin button: PullUpInput = 0
}

task blink {
    every 500ms {
        led.toggle()
    }
}

task motor_control {
    every 1ms strict {
        motor.set_pwm(0)
    }
}

task telemetry {
    every 1second {
        mqtt.send_int("drone/status", 1)
    }
}

setup {
    wifi.connect("ground", "station")
    run(blink, motor_control, telemetry)
}
