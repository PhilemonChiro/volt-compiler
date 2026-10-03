chip esp32

hardware {
    pin led = 2
    device env:   BME280
    device oled:  SSD1306 at 0x3D
    device rfid:  MFRC522(cs: 5)
    device radio: NRF24L01(cs: 15, ce: 4, channel: 90)
    device modem: SIM800L(tx: 17, rx: 16, baud: 115200)
    device strip: NeoPixel(pin: 13, leds: 30)
    device sonar: HCSR04(trig: 26, echo: 27)
    i2c sensors(sda: 32, scl: 33, speed: 100k)
    device env2:  BME280 on sensors at 0x77
    device lcd:   HD44780(cols: 20, rows: 4) on sensors
}

every 1s {
    led.toggle()
}
