chip esp32
hardware {
    pin led: Output = 2
    bus uart0: UART(tx: 17, rx: 16)
    device dev: SIM800L on uart0 at 0
}
every 1s {
    led.toggle()
    dev.ready()
}
