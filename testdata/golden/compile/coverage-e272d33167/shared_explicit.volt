
chip esp32
shared var counter: int = 0

task a {
    every 1s {
        counter += 1
    }
}
