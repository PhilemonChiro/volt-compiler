
chip esp32
hardware { pin led: Output = 2 }
task worker {
    var flag: bool = true
    if flag {
        var nested: int = 0
        every 1s {
            nested = nested + 1
            led.toggle()
        }
    }
}
