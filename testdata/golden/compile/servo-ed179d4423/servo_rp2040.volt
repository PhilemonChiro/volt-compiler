
chip rp2040

hardware {
    pin arm: Servo = 15
}

setup {
    arm.move(90)
    arm.ease_to(0, 1000ms)
}
