
chip esp32
hardware {
    pin led: Output = 2
}

shared var sample_count: int = 0
channel readings: int[16]

task sampler {
    every 100ms {
        readings.send(42)
        sample_count += 1
    }
}

task processor {
    every 50ms {
        if readings.has_data() {
            var val: int = readings.receive()
            if val > 40 {
                led.on()
            }
        }
    }
}

task monitor {
    every 5s {
        log("stats", "samples={sample_count}")
    }
}
