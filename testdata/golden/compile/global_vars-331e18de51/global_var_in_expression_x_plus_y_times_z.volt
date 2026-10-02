
var x: int = 1
var y: int = 2
var z: int = 3
task expr_task {
    every 1ms strict {
        x = x + y * z
    }
}
setup {
    run(expr_task)
}
