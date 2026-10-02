
setup {
    if x > 0 {
        log("positive")
    } else {
        log("non-positive")
    }
    while running {
        tick()
    }
    for i in 0..10 {
        log(i)
    }
}
