/* IoT sensor — WiFi + MQTT + battery monitor */

hardware {
    pin led = 2
    pin motor: PWM = 4
    pin battery: Analog = 36
}

setup {
    wifi.connect("MyNetwork", "MyPassword") or retry(10)
    mqtt.connect("mqtt://broker.local") or retry(5)
}

every 500ms { led.toggle() }

every 1ms strict {
    motor.set_pwm(0)
}

every 5s when wifi.is_connected() {
    mqtt.send_int("volt/battery", battery.read()) or retry(3)
}
