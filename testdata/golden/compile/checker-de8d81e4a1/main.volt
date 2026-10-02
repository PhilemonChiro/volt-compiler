
hardware {
    pin led: Output = 2
    pin battery: Analog = 36
}
func add(a: int, b: int) -> int {
    return a + b
}
task blink {
    every 500ms {
        led.toggle()
    }
}
setup {
    let x = 42
    let y = add(x, 1)
    let pi = 3.14
    let name = "sensor"
    let flag = true
    wifi.connect("net", "pass")
    mqtt.connect("mqtt://localhost")
    run(blink)
}
