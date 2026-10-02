
chip esp32
hardware {
    pin led: Output = 2
    pin btn: PullUpInput = 0
    pin pot: Analog = 34
    pin motor: PWM = 25
    pin arm: Servo = 13
    pin audio: DAC = 26
    pin pad: Touch = 4
}
setup {
    led.on()
    var pressed: bool = btn.is_pressed()
    var reading: int = pot.read()
    motor.write(128)
    arm.move(90)
    audio.write(128)
    var touched: bool = pad.is_touched()
}
