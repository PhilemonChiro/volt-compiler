
chip esp32
hardware {
    pin dac: DAC = 25
}
setup {
    var level: int = dac.read()
}
