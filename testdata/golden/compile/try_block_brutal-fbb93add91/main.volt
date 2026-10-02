chip esp32
hardware { pin led: Output = 4 }
var counter: int = 0
setup {
    try {
        counter += 1
        wifi.connect("a","b")
    } or retry 3 times else skip
}
every 1s { led.toggle() }
