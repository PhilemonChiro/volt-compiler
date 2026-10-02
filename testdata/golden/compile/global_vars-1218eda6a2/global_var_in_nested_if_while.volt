
var depth: int = 0
task deep {
    every 100ms {
        if depth > 0 {
            while depth > 0 {
                depth = depth - 1
            }
        }
    }
}
task isr {
    every 1ms strict {
        depth = depth + 1
    }
}
setup {
    run(deep, isr)
}
