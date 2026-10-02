chip esp32

hardware {
    pin led: Output = 4
}

var n = 0

every 1s {
    led.toggle()
    n = n + 1
}
