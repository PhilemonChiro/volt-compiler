
chip esp32
hardware { pin led: Output = 2 }
shared var counter: int = 0
task inc {
    every 100ms strict {
        counter += 1
    }
}
task dec {
    every 200ms strict {
        counter -= 1
    }
}
