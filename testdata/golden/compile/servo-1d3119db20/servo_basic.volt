
chip esp32

hardware {
    pin arm: Servo = 13
}

setup {
    arm.move(90)
}
