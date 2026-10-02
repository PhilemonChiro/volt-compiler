
chip esp32

hardware {
    pin led: Output = 2
    pin motor: PWM = 4
    pin button: PullUpInput = 0
    pin battery: Analog = 36
    pin soil: Analog = 39
}

setup {
    led.toggle()
    let bat = battery.read()
}
