
chip esp32
hardware {
    pin audio: DAC = 25
}
setup {
    audio.write(128)
}
