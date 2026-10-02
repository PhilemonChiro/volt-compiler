
chip esp32
channel data: int[1024]
task t {
    every 1s {
        data.send(1)
    }
}
