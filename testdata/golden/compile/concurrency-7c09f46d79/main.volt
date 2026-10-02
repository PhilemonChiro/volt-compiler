
chip esp32

hardware {
    pin led: Output = 2
    pin button: PullUpInput = 0
}

var counter = 0

task task_a {
    every 200ms {
        counter = counter + 1
        led.toggle()
    }
}

task task_b {
    every 300ms {
        counter = counter + 2
    }
}

setup {
    run(task_a, task_b)
}
