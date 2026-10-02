chip esp8266

hardware {
    pin led: Output = 2
}

task blink {
    var count: int = 0
    every 500ms {
        led.toggle()
        count += 1
        log("blink", "toggle #{count}")
    }
}
