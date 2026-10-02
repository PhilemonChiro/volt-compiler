
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

task net_handler {
    every 5seconds {
        wifi.connect("net", "pass")
    }
}

setup {
    run(blink, motor_pid, net_handler)
}
