
chip esp32
var total: int = 0

task a {
    every 1s {
        total += 1
    }
}
task b {
    every 2s {
        total += 10
    }
}
