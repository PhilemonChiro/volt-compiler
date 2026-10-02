
let max_speed: int = 100
var speed: int = 0
var shared_flag: int = 0
task control {
    every 1ms strict {
        shared_flag = 1
    }
}
task monitor {
    every 500ms {
        speed = shared_flag + max_speed
    }
}
setup {
    run(control, monitor)
}
