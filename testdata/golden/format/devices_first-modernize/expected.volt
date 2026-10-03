chip esp32

hardware {
    pin led: Output = 2
    device env: BME280 on i2c0 at 0x76
    device rfid: MFRC522(cs: 5) on spi0
    device modem: SIM800L on ser
    device sonar: HCSR04(trig: 26, echo: 27)
    i2c i2c0(sda: 21, scl: 22)
    spi spi0(mosi: 23, miso: 19, clk: 18)
    uart ser(tx: 17, rx: 16, baud: 9600)
}

every 1s { led.toggle() }
