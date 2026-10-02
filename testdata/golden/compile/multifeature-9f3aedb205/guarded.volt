
chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
}

task worker {
    var count: int = 0
    every 500ms when count < 100 {
        led1.toggle()
        count = count + 1
    }
}

task slow {
    every 2s {
        led2.toggle()
    }
}
