
chip esp32
hardware {
    pin led: Output = 2
    pin buzzer: PWM = 5
}

task heartbeat {
    every 1s {
        led.toggle() or skip
    }
}

task alert {
    every 5s {
        buzzer.set_pwm(128) or retry(3)
        sleep(200ms)
        buzzer.set_pwm(0) or skip
    }
}
