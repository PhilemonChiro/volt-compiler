
chip esp32
hardware {
    pin audio: DAC = 25
    pin ref: DAC = 26
}
setup {
    audio.write(128)
    audio.voltage(1.65)
    var level: int = audio.read()
    ref.write(255)
}
