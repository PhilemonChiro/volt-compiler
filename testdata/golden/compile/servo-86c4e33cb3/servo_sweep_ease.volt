
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.sweep_ease(0, 180, 2000ms, ease_bounce)
}
