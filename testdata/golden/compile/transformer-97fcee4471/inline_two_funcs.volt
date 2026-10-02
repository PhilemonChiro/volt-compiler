
chip esp32
hardware { pin led: Output = 2 }
func wait_a() {
    var x: int = 1
    sleep(50ms)
}
func wait_b() {
    var x: int = 2
    sleep(100ms)
}
task worker {
    every 1s {
        wait_a()
        wait_b()
    }
}
