hardware {
    pin alarm = 2
    device env: BME280
}

var mode: int = 0
var temp: float = 0.0
var alpha_on: bool = false
var bravo_on: bool = false
var charlie_on: bool = false
channel events: int[8]

func limit(x: int) -> int {
    return x + mode
}

func step() -> int {
    return limit(1)
}

setup {
    mode = 1
    log("boot", "ready")
}

every 1s {
    temp = env.temperature()
    events.send(1)
}

task watchdog {
    every 100ms {
        if step() > 2 {
            alarm.on()
        }
    }
}

task logger {
    every 500ms {
        if events.has_data() {
            let e = events.receive()
            let t = temp
            log("event", "got {e} at {t}")
        }
    }
}

machine line_alpha {
    state idle {
        enter { alpha_on = mode > 0 }
        after 1s -> idle
    }
}

machine line_bravo {
    state idle {
        enter { bravo_on = mode > 0 }
        after 1s -> idle
    }
}

machine line_charlie {
    state idle {
        enter { charlie_on = mode > 0 }
        after 1s -> idle
    }
}

every 2s {
    if alpha_on && bravo_on && charlie_on {
        mode = 2
    }
}
