/* ============================================================
Smart Door Lock — v3

WiFi-connected door lock with keypad, buzzer alerts,
auto-lock timer, and remote MQTT control.

Hardware:
GPIO2  = status LED
GPIO4  = lock relay
GPIO15 = keypad button
GPIO18 = buzzer (PWM)
============================================================ */

enum DoorState { locked, unlocked, alert }

hardware {
    pin led = 2
    pin relay = 4
    pin button: PullUpInput = 15
    pin buzzer: PWM = 18
}

storage total_unlocks: int = 0
storage failed_attempts: int = 0

var door: DoorState = DoorState.locked
var attempt_count: int = 0
var unlock_request: int = 0

channel lock_commands: int[4]

/* Startup */
setup {
    // wifi.connect("HomeNet", "password") or retry(10)
    // mqtt.connect("mqtt://192.168.1.100") or retry(5)
    log("boot", "Smart Door Lock v3")
    log("boot", "press button 3x within 2s to unlock")
    buzzer.beep_startup()
}

/* Keypad: detect 3-press unlock code */
task keypad {
    var last: int = 1
    var press_count: int = 0
    var last_press_time: int = 0

    every 30ms {
        let state: int = button.read()
        if state == 0 {
            if last == 1 {
                press_count += 1
                last_press_time = system.uptime()
                buzzer.click()

                if press_count >= 3 {
                    lock_commands.send(1)
                    press_count = 0
                }
            }
        }
        last = state

        let now: int = system.uptime()
        if now - last_press_time > 2 {
            if press_count > 0 {
                if press_count < 3 {
                    lock_commands.send(0)
                    buzzer.beep_error()
                }
                press_count = 0
            }
        }
    }
}

/* Lock state machine */
machine door_lock {
    var auto_lock_timer: int = 0

    state locked {
        enter {
            relay.off()
            led.off()
            door = DoorState.locked
            log("lock", "LOCKED")
            // mqtt.send_int("door/status", 0)
        }
        on unlock_request == 1 -> unlocked { unlock_request = 0 }
        on attempt_count >= 3 -> alert
    }

    state unlocked {
        enter {
            relay.on()
            led.on()
            door = DoorState.unlocked
            total_unlocks += 1
            log("lock", "UNLOCKED")
            // mqtt.send_int("door/status", 1)
            buzzer.beep_success()
        }
        after 10s -> locked
    }

    state alert {
        enter {
            buzzer.beep_alarm()
            log("lock", "ALERT — too many failed attempts!")
            // mqtt.send_int("door/alert", 1)
        }
        exit {
            attempt_count = 0
            // mqtt.send_int("door/alert", 0)
        }
        after 20s -> locked
    }
}

/* Command processor */
task command_processor {
    every 100ms {
        if lock_commands.has_data() {
            let cmd: int = lock_commands.receive()
            if cmd == 1 {
                attempt_count = 0
                unlock_request = 1
            } else {
                attempt_count += 1
                failed_attempts += 1
                log("security", "failed attempt #{attempt_count}")
            }
        }
    }
}

/* Remote status reporting */
// every 5s when wifi.is_connected() {
//     mqtt.send_int("door/locked", 0) or retry(3)
// }

/* Health + persistence */
every 30s {
    storage.save("total_unlocks", total_unlocks)
    storage.save("failed_attempts", failed_attempts)
    let heap: int = system.free_heap()
    log("health", "heap={heap} unlocks={total_unlocks}")
}
