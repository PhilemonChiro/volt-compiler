
chip esp32
hardware { pin led: Output = 2 }
channel queue: int[8]
task producer {
    every 1s { queue.send(42) }
}
task consumer {
    every 100ms {
        if queue.has_data() {
            var val: int = queue.receive()
        }
    }
}
