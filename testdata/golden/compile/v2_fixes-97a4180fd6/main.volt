chip rp2040
hardware {
    pin led: Output = 25
}
task blink {
    every 500ms {
        led.toggle()
    }
}
