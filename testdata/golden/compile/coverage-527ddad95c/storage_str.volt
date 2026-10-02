
chip esp32
hardware { pin led: Output = 2 }
setup {
    storage.save("name", "volt")
    var n: string = storage.load_str("name")
}
