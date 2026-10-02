
var x: int = 0
task bg {
    every 500ms {
        x = x + 1
    }
}
task isr {
    every 1ms strict {
        x = 0
    }
}
setup {
    run(bg, isr)
}
