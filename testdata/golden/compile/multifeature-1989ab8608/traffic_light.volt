
chip esp32
hardware {
    pin red: Output = 2
    pin yellow: Output = 4
    pin green: Output = 5
}

enum Light { off, red, yellow, green }

func all_off() {
    red.off()
    yellow.off()
    green.off()
}

setup {
    var current: Light = Light.red
    all_off()
    match(current) {
        .red -> red.on()
        .yellow -> yellow.on()
        .green -> green.on()
        _ -> all_off()
    }
}
