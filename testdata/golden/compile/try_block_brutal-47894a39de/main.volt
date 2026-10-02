chip esp32
hardware { pin led: Output = 4 }
task tick {
    every 1ms strict {
        try { wifi.connect("a","b") } or skip
    }
}
