chip esp32
hardware {
    pin led: Output = 2
}
loop {
    led.toggle()
    sleep(500ms)
}
