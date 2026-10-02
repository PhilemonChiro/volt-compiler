chip esp32
hardware { pin led: Output = 4 }
machine m {
    state s {
        enter { try { wifi.connect("a","b") } or retry }
    }
}
