
chip esp32
channel data: int[16]
shared var total: int = 0

task producer {
    every 1s {
        total += 1
        data.send(total)
    }
}

task consumer {
    every 500ms {
        if data.has_data() {
            var val: int = data.receive()
            total -= val
        }
    }
}
