
chip esp32
hardware {
    pin pad: Touch = 4
}
task sense {
    every 100ms {
        if pad.is_touched() {
            log("touch", "detected")
        }
    }
}
