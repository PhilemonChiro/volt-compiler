
chip esp32

hardware {
    pin led: Output = 2
    pin button: PullUpInput = 0
    pin motor: PWM = 4
    pin battery: Analog = 36
}

storage boot_count: int = 0

func clamp(val: int, lo: int, hi: int) -> int {
    if val < lo {
        return lo
    }
    if val > hi {
        return hi
    }
    return val
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

task monitor {
    var count = 0
    every 5seconds {
        count = count + 1
        let bat = battery.read()
        let pct = clamp(bat, 0, 4095)
        mqtt.send_int("volt/battery", pct)
        if count > 100 {
            count = 0
        }
    }
}

setup {
    wifi.connect("MyNet", "MyPass")
    mqtt.connect("mqtt://broker.local")
    run(blink, motor_control, monitor)
}
