chip esp32
hardware { pin led: Output = 4 }
func go() {
    sleep(10ms)
    go()
}
task t { go() }
