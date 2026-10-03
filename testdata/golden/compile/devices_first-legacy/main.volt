chip esp32

hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    bus spi0: SPI(mosi: 23, miso: 19, clk: 18)
    bus ser: UART(tx: 17, rx: 16, baud: 9600)
    device env: BME280 on i2c0 at 0x76
    device rfid: MFRC522 on spi0 at 5
    device modem: SIM800L on ser at 0
    device sonar: HCSR04(trig: 26, echo: 27)
}

every 1s {
    led.toggle()
}
