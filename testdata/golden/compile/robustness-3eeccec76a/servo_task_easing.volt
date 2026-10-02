
chip esp32
hardware {
    pin arm: Servo = 13
    pin base: Servo = 14
}

task wave {
    every 3s {
        arm.ease_to_with(0, 1s, ease_out)
        arm.ease_to_with(180, 1s, ease_in)
        base.sweep(0, 90, 2s)
    }
}
