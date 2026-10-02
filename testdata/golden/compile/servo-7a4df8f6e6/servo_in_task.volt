
chip esp32

hardware {
    pin arm: Servo = 13
}

task waver {
    every 2s {
        arm.ease_to(0, 500ms)
        arm.ease_to(180, 500ms)
    }
}
