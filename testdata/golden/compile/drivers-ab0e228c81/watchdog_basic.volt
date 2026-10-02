
chip esp32
setup {
    system.watchdog_enable(5000)
}
task heartbeat {
    every 2s {
        system.watchdog_feed()
    }
}
