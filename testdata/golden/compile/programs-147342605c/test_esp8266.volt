chip esp8266

hardware {
    pin led: Output = 2
    pin relay: Output = 4
    pin button: PullUpInput = 5
}

task blink {
    every 500ms {
        led.toggle()
    }
}

task check_button {
    every 50ms {
        if button.read() == 0 {
            relay.on()
        } else {
            relay.off()
        }
    }
}

task report {
    every 30s {
        mqtt.send_int("device/status", 1)
    }
}

setup {
    wifi.connect("TestNet", "password")
    mqtt.connect("mqtt://broker.local")
}
