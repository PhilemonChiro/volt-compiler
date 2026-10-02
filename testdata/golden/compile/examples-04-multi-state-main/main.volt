/* Multiple state machines + tasks */

hardware {
    pin led = 2
}

machine pattern {
    var cycle: int = 0

    state fast {
        enter {
            log("mode", "fast")
        }
        after 3s -> slow
    }

    state slow {
        enter {
            log("mode", "slow")
        }
        after 5s -> pulse
    }

    state pulse {
        enter {
            log("mode", "pulse")
        }
        after 2s -> fast
    }
}

every 200ms { led.toggle() }

task status {
    var tick: int = 0
    every 1s {
        tick = tick + 1
        log("health", "alive")
    }
}
