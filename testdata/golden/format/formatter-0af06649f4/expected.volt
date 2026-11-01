chip esp32

hardware {
    pin led: Output = 2
}

setup {
    var x: int = 0
    x = x + 1
    log("app", "done")
}

task blink {
    every 500ms { led.toggle() }
}
