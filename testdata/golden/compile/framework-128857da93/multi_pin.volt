
chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
    pin btn: PullUpInput = 0
}
task lights {
    every 1s {
        led1.toggle()
        led2.toggle()
        var state: int = btn.read()
    }
}
