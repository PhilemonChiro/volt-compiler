chip esp8266

hardware {
    pin led: Output = 2
    pin relay: Output = 4
    pin sensor_pin: Input = 5
}

var heartbeat_count: int = 0
var sensor_reads: int = 0
var mqtt_sends: int = 0
var push_count: int = 0
var sync_count: int = 0

task heartbeat {
    every 1s {
        led.toggle()
        heartbeat_count += 1
        if heartbeat_count % 10 == 0 {
            log("heartbeat", "#" + heartbeat_count + " heap=" + system.free_heap())
        }
    }
}

task sensor_check {
    every 5s {
        let val = sensor_pin.read()
        sensor_reads += 1
        log("sensor", "#" + sensor_reads + " GPIO5=" + val)
        if val == 1 {
            relay.on()
        } else {
            relay.off()
        }
    }
}

task mqtt_report {
    every 30s {
        mqtt_sends += 1
        log("mqtt", "#" + mqtt_sends + " sending status + sensor")
        mqtt.send_int("esp8266/status", 1)
        mqtt.send_int("esp8266/sensor", sensor_pin.read())
    }
}

task data_buffer {
    every 10s {
        push_count += 1
        storage.push("readings", sensor_pin.read())
        log("storage", "push #" + push_count + " | buffered=" + storage.list_count("readings"))
    }
}

task sync {
    every 60s {
        if wifi.is_connected() {
            sync_count += 1
            log("sync", "#" + sync_count + " wifi connected, sending")
            mqtt.send_int("esp8266/alive", 1)
        } else {
            log("sync", "wifi disconnected, skipping")
        }
    }
}

task health {
    every 15s {
        log("health", "heartbeats=" + heartbeat_count + " sensor_reads=" + sensor_reads + " mqtt=" + mqtt_sends + " pushes=" + push_count + " syncs=" + sync_count)
        log("health", "wifi=" + wifi.is_connected() + " heap=" + system.free_heap())
    }
}

setup {
    wifi.connect("HomeNet", "password")
    mqtt.connect("mqtt://192.168.1.100")
}
