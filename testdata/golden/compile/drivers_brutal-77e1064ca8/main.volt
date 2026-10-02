chip esp32
hardware {
    pin led: Output = 2
    bus spi0: SPI(sck: 18, mosi: 23, miso: 19)
    device dev: W25Q on spi0 at 5
}
every 1s {
    led.toggle()
}
