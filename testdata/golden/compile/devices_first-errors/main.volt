chip esp32

hardware {
    pin led = 21
    device a:     BME280
    device b:     BME280
    device c:     BME280 at 0x50
    device rfid:  MFRC522 at 5
    device strip: NeoPixel(pin: 13, leds: "many", color: 3)
    device sonar: HCSR04(trig: 4)
    device beep:  DHT22(pin: 4)
    device m1:    SIM800L(tx: 17, rx: 16)
    device m2:    SIM800L(tx: 25, rx: 26)
    device m3:    SIM800L(tx: 32, rx: 34)
    i2c fast(sda: 18, scl: 19, speed: 400k)
    device lcd:   HD44780 on fast
}

every 1s {
    led.toggle()
}
