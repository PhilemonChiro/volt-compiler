chip esp32

hardware {
    pin p1: Output = 2
    pin p2: Output = 4
    pin p3: Output = 5
    pin p4: Output = 13
    pin p5: Output = 14
    pin p6: Output = 16
    pin p7: Output = 17
    pin p8: Output = 18
    pin p9: Output = 19
    pin p10: Output = 21
    pin p11: Output = 22
    pin p12: Output = 23
    pin a1: Analog = 32
    pin a2: Analog = 33
    pin a3: Analog = 34
    pin a4: Analog = 35
    pin a5: Analog = 36
    pin a6: Analog = 39
}

var t1_count: int = 0
var t2_count: int = 0
var t3_count: int = 0
var t4_count: int = 0
var t5_count: int = 0
var t6_count: int = 0
var t7_count: int = 0
var t8_count: int = 0
var t9_count: int = 0
var t10_count: int = 0
var isr1_count: int = 0
var isr2_count: int = 0
var adc_count: int = 0
var net_count: int = 0

task t1 {
    every 100ms {
        p1.toggle()
        t1_count += 1
    }
}

task t2 {
    every 200ms {
        p2.toggle()
        t2_count += 1
    }
}

task t3 {
    every 300ms {
        p3.toggle()
        t3_count += 1
    }
}

task t4 {
    every 400ms {
        p4.toggle()
        t4_count += 1
    }
}

task t5 {
    every 500ms {
        p5.toggle()
        t5_count += 1
    }
}

task t6 {
    every 600ms {
        p6.toggle()
        t6_count += 1
    }
}

task t7 {
    every 700ms {
        p7.toggle()
        t7_count += 1
    }
}

task t8 {
    every 800ms {
        p8.toggle()
        t8_count += 1
    }
}

task t9 {
    every 900ms {
        p9.toggle()
        t9_count += 1
    }
}

task t10 {
    every 1s {
        p10.toggle()
        t10_count += 1
    }
}

task isr1 {
    every 1ms strict {
        p11.toggle()
        isr1_count += 1
    }
}

task isr2 {
    every 500ms strict {
        p12.toggle()
        isr2_count += 1
    }
}

task adc_poll {
    every 100ms {
        let v1 = a1.read()
        let v2 = a2.read()
        let v3 = a3.read()
        let v4 = a4.read()
        let v5 = a5.read()
        let v6 = a6.read()
        adc_count += 1
        if adc_count % 50 == 0 {
            log("adc", "#" + adc_count + " ch0=" + v1 + " ch1=" + v2 + " ch2=" + v3 + " ch3=" + v4 + " ch4=" + v5 + " ch5=" + v6)
        }
    }
}

task net_report {
    every 10s {
        net_count += 1
        mqtt.send_int("stress/a1", a1.read())
        mqtt.send_int("stress/a2", a2.read())
        mqtt.send_int("stress/a3", a3.read())
        log("net", "#" + net_count + " mqtt sent 3 values")
    }
}

task stress_report {
    every 5s {
        log("stress", "t1=" + t1_count + " t2=" + t2_count + " t3=" + t3_count + " t4=" + t4_count + " t5=" + t5_count)
        log("stress", "t6=" + t6_count + " t7=" + t7_count + " t8=" + t8_count + " t9=" + t9_count + " t10=" + t10_count)
        log("stress", "isr1=" + isr1_count + " isr2=" + isr2_count + " adc=" + adc_count + " net=" + net_count)
        log("stress", "heap=" + system.free_heap() + " wifi=" + wifi.is_connected())
    }
}

setup {
    wifi.connect("StressNet", "password")
    mqtt.connect("mqtt://broker.local")
}
