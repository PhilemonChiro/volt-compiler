chip esp32
hardware {
    pin led: Output = 2
    pin gpio_pin: Output = 4
    device dht22: DHT22 on gpio_pin at 4
}
every 1s {
    led.toggle()
}
