chip esp32
hardware { pin led: Output = 4 }
var counter = 0
task tick {
    every 1ms strict {
        counter = counter + 1
    }
}
task slow {
    every 1s { led.toggle() }
}
