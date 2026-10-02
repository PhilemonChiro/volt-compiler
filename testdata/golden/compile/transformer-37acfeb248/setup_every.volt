
chip esp32
hardware { pin led: Output = 2 }
setup {
    led.on()
    every 500ms {
        led.toggle()
    }
}
