chip rp2040

hardware {
    pin led = 25
    device env: BME280
    device rfid: MFRC522(cs: 17)
    device modem: SIM800L
}

every 1s {
    led.toggle()
}
