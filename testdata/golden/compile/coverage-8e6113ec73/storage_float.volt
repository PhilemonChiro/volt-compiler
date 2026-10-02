
chip esp32
hardware { pin led: Output = 2 }
setup {
    storage.save("temp", 98.6)
    var t: float = storage.load_float("temp")
}
