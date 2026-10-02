chip esp32
hardware {
    pin led: Output = 2
    pin gpio_pin: Output = 4
    device neopixel: NeoPixel on gpio_pin at 4
}
every 1s {
    led.toggle()
}
