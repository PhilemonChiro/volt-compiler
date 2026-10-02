chip esp32
hardware {
    pin led: Output = 2
    bus uart0: UART(tx: 17, rx: 16)
    device dev: HC05 on uart0 at 0
}
every 1s {
    led.toggle()
    dev.read_at_response(0, 0)
}
