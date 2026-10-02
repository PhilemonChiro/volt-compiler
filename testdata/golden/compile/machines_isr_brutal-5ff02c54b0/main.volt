chip esp32
hardware { pin led: Output = 4 }
task t {
    every 1ms strict {
        led.toggle()
        sleep(10ms)
    }
}
