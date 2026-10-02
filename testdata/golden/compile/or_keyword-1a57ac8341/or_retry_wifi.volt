
chip esp32
setup {
    wifi.connect("net", "pass") or retry(3)
}
