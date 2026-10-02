
chip esp32

hardware {
    pin led: Output = 2
    pin battery: Analog = 36
}

storage boot_count: int = 0

struct Reading {
    temperature: float
    humidity: float
}

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

task monitor {
    var count = 0
    every 5seconds {
        count = count + 1
        let bat = battery.read()
        mqtt.send_int("volt/bat", clamp(bat, 0, 4095))
    }
}

setup {
    wifi.connect("MyNet", "MyPass")
    mqtt.connect("mqtt://broker.local")
    run(blink, monitor)
}
