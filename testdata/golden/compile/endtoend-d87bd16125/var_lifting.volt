
task monitor {
    var count = 0
    every 1seconds {
        count = count + 1
        log("tick")
    }
}
