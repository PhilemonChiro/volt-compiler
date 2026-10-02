
chip esp32
hardware { pin led: Output = 2 }
func delayed_set(val: int) {
    var x: int = val
    sleep(100ms)
    x = x + 1
}
task worker {
    every 1s {
        delayed_set(42)
    }
}
