chip esp32
hardware { pin led: Output = 4 }
var big: int64 = 0
task isr_task {
    every 1ms strict { big = big + 1 }
}
task slow_task {
    every 1s { led.toggle() }
}
