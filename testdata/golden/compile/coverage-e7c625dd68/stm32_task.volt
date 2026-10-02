
chip stm32f103
hardware {
    pin led: Output = 13
}
task blink {
    every 500ms {
        led.toggle()
    }
}
