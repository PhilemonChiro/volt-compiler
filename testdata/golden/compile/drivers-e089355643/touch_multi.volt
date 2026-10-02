
chip esp32
hardware {
    pin pad1: Touch = 4
    pin pad2: Touch = 15
    pin pad3: Touch = 27
}
task sense {
    every 50ms {
        if pad1.is_touched() {
            log("touch", "pad1")
        }
        if pad2.is_touched() {
            log("touch", "pad2")
        }
    }
}
