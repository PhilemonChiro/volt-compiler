chip esp32

hardware {
    pin button: PullUpInput = 23
}

func link() {
    mqtt.connect("mqtt://broker.local") or skip
}

setup {
    wifi.connect("home", "secret") or retry(5)
    mqtt.connect("mqtt://broker.local") or skip
    timeout 5s {
        wifi.connect("home", "secret") or skip
    } else {
        log("wifi", "gave up after 5 seconds")
    }
}

on button.is_pressed() {
    link()
    mqtt.connect("mqtt://broker.local") or skip
}

every 1s {
    mqtt.connect("mqtt://broker.local") or skip
}
