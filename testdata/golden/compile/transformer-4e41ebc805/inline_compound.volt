
chip esp32
hardware { pin led: Output = 2 }
func inc() {
    var counter: int = 0
    counter += 1
    sleep(50ms)
}
task worker {
    every 500ms { inc() }
}
