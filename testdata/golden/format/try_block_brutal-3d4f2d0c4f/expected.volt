chip esp32

hardware {
    pin led: Output = 4
}

every 1s { led.toggle() }

setup {
    try {
        wifi.connect("a", "b")
    } or skip
}
