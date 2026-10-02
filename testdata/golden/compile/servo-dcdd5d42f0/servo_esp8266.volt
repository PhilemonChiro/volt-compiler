
chip esp8266

hardware {
    pin arm: Servo = 14
}

setup {
    arm.move(45)
}
