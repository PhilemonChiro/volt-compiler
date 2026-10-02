
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.ease_to(180, 1000ms)
}
