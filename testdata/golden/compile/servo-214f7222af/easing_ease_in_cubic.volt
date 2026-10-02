
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.ease_to_with(90, 500ms, ease_in_cubic)
}
