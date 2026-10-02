
var ticks: int = 0
task ticker {
    every 500ms {
        ticks = ticks + 1
    }
}
setup {
    run(ticker)
}
