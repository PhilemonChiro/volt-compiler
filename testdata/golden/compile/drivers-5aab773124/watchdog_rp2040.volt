
chip rp2040
setup {
    system.watchdog_enable(8000)
}
task alive {
    every 3s {
        system.watchdog_feed()
    }
}
