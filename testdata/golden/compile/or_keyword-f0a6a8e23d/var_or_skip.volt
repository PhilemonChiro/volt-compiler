
chip esp32
hardware { pin btn: Input = 4 }
task reader {
    every 2s {
        var val: int = btn.read() or skip
    }
}
