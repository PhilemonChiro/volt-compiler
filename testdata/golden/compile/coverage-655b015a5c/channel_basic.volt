
chip esp32
channel readings: int[16]

task producer {
    every 1s {
        readings.send(42)
    }
}

task consumer {
    every 500ms {
        if readings.has_data() {
            var val: int = readings.receive()
        }
    }
}
