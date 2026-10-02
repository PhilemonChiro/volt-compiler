
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.ease_to_with(180, 1000ms, ease_out)
}
