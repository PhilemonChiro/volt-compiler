
chip esp32
hardware {
    bus com: UART(tx: 17, rx: 16, baud: 115200)
}
setup {
    com.print("hello")
}
