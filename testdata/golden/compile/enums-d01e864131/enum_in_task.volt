
chip esp32
enum AlertLevel {
    normal
    warning
    critical
}
hardware {
    pin buzzer: PWM = 25
}
task monitor {
    every 5s {
        var level: AlertLevel = AlertLevel.normal
        if level == AlertLevel.critical {
            buzzer.beep_alarm()
        }
    }
}
