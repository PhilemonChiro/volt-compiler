
task monitor {
    var count = 0
    every 5seconds {
        count = count + 1
        let temp = 23
        log("done")
    }
}
