chip esp32
hardware {
    pin led: Output = 2
    pin trig: Output = 5
    device dist: HCSR04 on trig at 5
}
every 1s { led.toggle() }
