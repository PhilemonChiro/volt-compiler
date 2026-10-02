
chip esp32
setup {
    var temp: float = 25.5
    if temp > 30 {
        log("hot", "yes")
    }
    if temp <= 20 {
        log("cold", "yes")
    }
    if 100 >= temp {
        log("normal", "yes")
    }
}
