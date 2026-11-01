chip esp32

hardware {
    pin led: Output = 2
}

every 1s {
    mqtt.connect("mqtt://broker.local") or skip
    led.toggle() or skip
}

every 500ms {
    for i in 0..3 {
        led.toggle() or skip
    }
    led.toggle()
}
