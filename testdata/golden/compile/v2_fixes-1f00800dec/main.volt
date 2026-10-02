chip esp32
hardware { pin led: Output = 2 }
task worker {
    led.on()
    every 500ms { led.toggle() }
    every 1s { log("tick", "one") }
}
