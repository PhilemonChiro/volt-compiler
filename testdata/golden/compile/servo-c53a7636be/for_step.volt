
chip esp32

hardware {
    pin arm: Servo = 5
}

setup {
    for i in 0..180 step 10 {
        arm.move(i)
        sleep(100ms)
    }
}
