
chip esp32
hardware { pin led = 2 }
task main {
    var msg: string = "hello %x %x"
    log("app", msg)
}
