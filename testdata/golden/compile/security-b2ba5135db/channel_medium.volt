
chip esp32
channel data: int[512]
task t {
    every 1s {
        data.send(1)
    }
}
