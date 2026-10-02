chip esp8266

hardware {
    pin led: Output = 2
    pin relay: Output = 4
    pin buzzer: Output = 14
}

var lock_toggles: int = 0
var blink_count: int = 0

machine DoorController {
    initial state locked {
        enter {
            relay.off()
            led.off()
            log("door", "STATE -> locked")
        }
        on unlock_cmd -> unlocked
    }

    state unlocked {
        enter {
            relay.on()
            led.on()
            lock_toggles += 1
            log("door", "STATE -> unlocked #" + lock_toggles + " | auto-lock in 10s")
        }
        after 10s -> locked
        on lock_cmd -> locked
    }
}

task status_blink {
    every 2s {
        buzzer.toggle()
        blink_count += 1
        log("status", "blink #" + blink_count + " | lock_toggles=" + lock_toggles)
    }
}

task health {
    every 10s {
        log("health", "lock_toggles=" + lock_toggles + " blinks=" + blink_count + " wifi=" + wifi.is_connected() + " heap=" + system.free_heap())
    }
}

setup {
    wifi.connect("DoorNet", "password")
}
