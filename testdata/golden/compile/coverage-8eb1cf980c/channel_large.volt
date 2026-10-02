
chip esp32
channel big: int[300]
task t {
    every 1s {
        big.send(1)
    }
}
