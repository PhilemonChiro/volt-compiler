chip esp32
hardware { pin led: Output = 4 }
setup {
    try {
        try {
            wifi.connect("a","b")
        } or retry 2 times else fail
        storage.load("config")
    } or retry 3 times else restart
}
every 1s { led.toggle() }
