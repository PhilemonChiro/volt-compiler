
chip esp32
channel data: int[9999]
task t {
    every 1s {
        data.send(1)
    }
}
