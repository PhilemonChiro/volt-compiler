
chip esp32
setup {
    var mem: int = system.free_heap()
    var up: int = system.uptime()
    system.restart()
}
