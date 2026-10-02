
var counter: int = 0
task slow_task {
    every 1seconds {
        counter = counter + 1
    }
}
task fast_task {
    every 1ms strict {
        counter = counter + 1
    }
}
setup {
    run(slow_task, fast_task)
}
