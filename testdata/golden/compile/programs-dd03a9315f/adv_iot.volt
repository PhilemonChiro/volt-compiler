chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
    pin battery: Analog = 36
    pin temp: Analog = 39
}

var heartbeat_count: int = 0
var motor_isr_count: int = 0
var telemetry_count: int = 0
var cloud_count: int = 0

task heartbeat {
    every 1s {
        led.toggle()
        heartbeat_count += 1
        log("heartbeat", "#" + heartbeat_count + " | heap=" + system.free_heap())
    }
}

task motor_control {
    every 1ms strict {
        motor.set_pwm(0)
        motor_isr_count += 1
    }
}

task telemetry {
    every 30s {
        let bat = battery.read()
        let t = temp.read()
        telemetry_count += 1
        log("telemetry", "#" + telemetry_count + " bat=" + bat + " temp=" + t)
        mqtt.send_int("device/battery", bat)
        mqtt.send_int("device/temp", t)
    }
}

task cloud_report {
    every 60s {
        cloud_count += 1
        log("cloud", "#" + cloud_count + " posting to API")
        http.post("https://api.example.com/data", "ok")
    }
}

task stats {
    every 10s {
        log("stats", "heartbeat=" + heartbeat_count + " isr=" + motor_isr_count + " telemetry=" + telemetry_count + " cloud=" + cloud_count)
        log("stats", "wifi=" + wifi.is_connected() + " heap=" + system.free_heap())
    }
}

setup {
    wifi.connect("TestNetwork", "TestPassword")
    mqtt.connect("mqtt://broker.local")
}
