
chip esp32
hardware {
    pin led: Output = 2
    pin motor: PWM = 4
}
task blink {
    every 500ms {
        led.toggle()
    }
}
task motor_pid {
    every 1ms strict {
        motor.set_pwm(0)
    }
}
task telemetry {
    every 5seconds {
        mqtt.send_int("status", 1)
    }
}
setup {
    wifi.connect("net", "pass")
    mqtt.connect("mqtt://broker")
    run(blink, motor_pid, telemetry)
}
