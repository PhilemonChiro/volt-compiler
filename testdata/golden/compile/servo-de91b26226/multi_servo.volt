
chip esp32

hardware {
    pin base: Servo = 13
    pin shoulder: Servo = 14
    pin elbow: Servo = 15
}

setup {
    base.move(90)
    shoulder.move(45)
    elbow.move(135)
}
