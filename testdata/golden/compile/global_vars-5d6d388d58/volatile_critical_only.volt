
var isr_count: int = 0
task isr_task {
    every 1ms strict {
        isr_count = isr_count + 1
    }
}
setup {
    run(isr_task)
}
