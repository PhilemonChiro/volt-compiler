
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.sweep(0, 180, 2000ms)
}
