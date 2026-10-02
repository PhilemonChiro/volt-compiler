
chip rp2040
hardware {
    pin dac: DAC = 15
}
setup {
    dac.write(100)
}
