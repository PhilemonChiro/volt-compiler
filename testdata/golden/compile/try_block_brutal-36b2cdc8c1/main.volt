chip esp32
hardware { pin led: Output = 4 }
setup {
    try {
        led.on()
        wifi.connect("a","b")
    } or retry 3 times else skip
}
every 1s { led.toggle() }
