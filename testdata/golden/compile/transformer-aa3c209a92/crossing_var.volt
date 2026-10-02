
chip esp32
hardware { pin led: Output = 2 }
task counter {
    var count: int = 0
    every 1s {
        count = count + 1
        if count > 10 {
            led.on()
        }
    }
}
