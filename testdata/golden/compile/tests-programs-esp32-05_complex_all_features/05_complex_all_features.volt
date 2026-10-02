chip esp32

hardware {
    pin led_r: Output = 23
    pin led_g: Output = 22
    pin led_b: Output = 21
    pin motor: PWM = 4
    pin fan: Output = 5
    pin button: PullUpInput = 15
    pin soil: Analog = 32
    pin temp: Analog = 33
    pin light: Analog = 34
}

var setpoint: int = 500
var fan_on: int = 0
var total_sensor_reads: int = 0
var total_pushes: int = 0
var total_syncs: int = 0
var motor_isr_count: int = 0
var button_presses: int = 0
var last_button: int = 1

func check_threshold(value: int, limit: int) -> bool {
    return value > limit
}

task sensor_poll {
    every 2s {
        let s = soil.read()
        let t = temp.read()
        let l = light.read()
        total_sensor_reads += 1

        log("sensor", "#" + total_sensor_reads + " soil=" + s + " temp=" + t + " light=" + l)

        if check_threshold(t, 800) {
            fan.on()
            fan_on = 1
        } else {
            fan.off()
            fan_on = 0
        }

        if s < setpoint {
            motor.set_pwm(200)
        } else {
            motor.set_pwm(0)
        }
    }
}

task pid_control {
    every 1ms strict {
        motor.set_pwm(0)
        motor_isr_count += 1
    }
}

task button_monitor {
    every 50ms {
        let state = button.read()
        if state == 0 {
            if last_button == 1 {
                button_presses += 1
                log("button", "PRESS #" + button_presses)
            }
        }
        last_button = state
    }
}

task data_logger {
    every 30s {
        storage.push("soil", soil.read())
        storage.push("temp", temp.read())
        storage.push("light", light.read())
        total_pushes += 3
        log("storage", "pushed 3 readings | total=" + total_pushes)
    }
}

task cloud_sync {
    every 60s {
        if wifi.is_connected() {
            total_syncs += 1
            log("sync", "#" + total_syncs + " sending")
            mqtt.send_int("farm/soil", soil.read())
            mqtt.send_int("farm/temp", temp.read())
            mqtt.send_int("farm/light", light.read())
            mqtt.send_int("farm/fan", fan_on)
        } else {
            log("sync", "wifi not connected, data buffered")
        }
    }
}

task health_report {
    every 10s {
        log("health", "reads=" + total_sensor_reads + " isr=" + motor_isr_count + " btn=" + button_presses + " pushes=" + total_pushes + " syncs=" + total_syncs)
        log("health", "wifi=" + wifi.is_connected() + " heap=" + system.free_heap() + " fan=" + fan_on)
    }
}

machine StatusLED {
    initial state off {
        enter {
            led_r.off()
            led_g.off()
            led_b.off()
        }
        on wifi_connected -> connected
    }

    state connected {
        enter {
            led_g.on()
            log("led_fsm", "STATE -> connected")
        }
        exit {
            led_g.off()
        }
        on wifi_lost -> disconnected
        on error_event -> error
    }

    state disconnected {
        enter {
            led_r.on()
            log("led_fsm", "STATE -> disconnected")
        }
        exit {
            led_r.off()
        }
        after 5s -> off
        on wifi_connected -> connected
    }

    state error {
        enter {
            led_r.on()
            led_b.on()
            log("led_fsm", "STATE -> error")
        }
        exit {
            led_r.off()
            led_b.off()
        }
        after 10s -> off
    }
}

setup {
    wifi.connect("FarmNet", "password")
    mqtt.connect("mqtt://broker.local")
}
