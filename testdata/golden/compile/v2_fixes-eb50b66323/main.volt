chip esp32
hardware { pin led: Output = 2 }
func count_up() {
    var x: int = 0
    x += 1
    sleep(100ms)
}
task worker {
    every 1s { count_up() }
}
