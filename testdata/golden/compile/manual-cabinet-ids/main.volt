/* ============================================================
Cabinet Intrusion Detection System — node firmware

ESP32 DevKit V1
  GPIO27 = reed door sensor (other leg to GND)
  GPIO21/22 = I2C: GY-85 (ADXL345 0x53, ITG3205 0x68, HMC5883L 0x1E)
               + 16x2 LCD backpack (0x27)
  GPIO16/17 = UART to SIM800L (own 4.0V supply, common GND)
  GPIO2  = on-board LED
============================================================ */

hardware {
    pin led = 2
    pin door: PullUpInput = 27

    bus i2c0: I2C(sda: 21, scl: 22)
    bus gsm_uart: UART(tx: 17, rx: 16, baud: 9600)

    device lcd: LCD16x2 on i2c0 at 0x27
    device accel: ADXL345 on i2c0 at 0x53
    device gyro: ITG3205 on i2c0 at 0x68
    device gsm: SIM800L on gsm_uart at 0
}

enum Alarm { armed, intrusion, tamper, cooldown }

storage alert_count: int = 0

var door_open: int = 0
var shock: int = 0          // peak |accel - 1g| in the last window, x100
var spin: int = 0           // peak gyro rate in the last window, deg/s
var net_ok: int = 0
var sig: int = 0
var alarm: Alarm = Alarm.armed

/* Fast sensor sampling: track peaks, decay them every 2 s so a single
   knock doesn't hold the alarm */
task sensing {
    var tick: int = 0

    every 50ms {
        door_open = door.read()

        let m: float = accel.magnitude()
        var dev: float = m - 1.0
        if dev < 0.0 { dev = 0.0 - dev }
        let s: int = int(dev * 100.0)
        if s > shock { shock = s }

        let g: int = int(gyro.magnitude())
        if g > spin { spin = g }

        tick += 1
        if tick >= 40 {
            tick = 0
            shock = shock * 7 / 10
            spin = spin * 7 / 10
        }
    }
}

/* Alarm state machine */
machine detector {
    state armed {
        enter {
            alarm = Alarm.armed
            led.off()
            log("ids", "ARMED")
        }
        on door_open == 1 -> intrusion
        on shock > 60 -> tamper       // > 0.6 g impact
        on spin > 90 -> tamper        // > 90 deg/s
    }

    state intrusion {
        enter {
            alarm = Alarm.intrusion
            led.on()
            alert_count += 1
            storage.save("alert_count", alert_count)
            log("ids", "INTRUSION — door open, alert #{alert_count}")
            gsm.send_sms("+263771234567", "CABINET ALERT: door opened")
        }
        after 5s -> cooldown
    }

    state tamper {
        enter {
            alarm = Alarm.tamper
            led.on()
            alert_count += 1
            storage.save("alert_count", alert_count)
            log("ids", "TAMPER — shock={shock} spin={spin}, alert #{alert_count}")
            gsm.send_sms("+263771234567", "CABINET ALERT: tampering detected")
        }
        after 5s -> cooldown
    }

    /* Wait for the door to close and things to settle before re-arming,
       so one event produces one SMS. */
    state cooldown {
        enter {
            alarm = Alarm.cooldown
            log("ids", "cooldown")
        }
        on door_open == 0 && shock < 20 && spin < 30 -> armed
    }
}

/* Network health */
every 30s {
    net_ok = 0
    if gsm.registered() { net_ok = 1 }
    sig = gsm.signal()
    log("gsm", "registered={net_ok} csq={sig}")
}

/* Signal icon in the last two cells (custom chars 1-2), right-aligned.
   Cell 1: 3-px antenna on columns 0-2, bar 1 on column 4.
   Cell 2: bars 2-4 on columns 0, 2, 4. The gap between LCD cells stands in
   for the blank column, so all four 1-px bars are evenly spaced.
   CSQ -> bars: <10 one, 10-14 two, 15-19 three, 20+ four.
   No modem / unregistered / CSQ 99 -> an X in cell 2. */
var bars_shown: int = -2

func csq_bars(csq: int, reg: int) -> int {
    if reg == 0 || csq == 99 { return -1 }
    if csq < 2 { return 0 }
    if csq < 10 { return 1 }
    if csq < 15 { return 2 }
    if csq < 20 { return 3 }
    return 4
}

/* Bar n is 2n pixels tall; an unlit bar keeps a 1-pixel stub on row 7 */
func bar_px(r: int, lvl: int, bar: int, bits: int) -> int {
    if lvl >= bar && r >= 8 - bar * 2 { return bits }
    if r == 7 { return bits }
    return 0
}

/* Antenna on columns 0-2: arms on row 0, mast down the middle */
func antenna_row(r: int) -> int {
    if r == 0 { return 20 }
    if r == 1 { return 28 }
    return 8
}

/* 5x5 X sitting on the baseline (rows 3-7) */
func x_row(r: int) -> int {
    if r == 3 || r == 7 { return 17 }
    if r == 4 || r == 6 { return 10 }
    if r == 5 { return 4 }
    return 0
}

func cell_row(r: int, lvl: int, cell: int) -> int {
    if cell == 1 {
        if lvl < 0 { return antenna_row(r) }
        return antenna_row(r) + bar_px(r, lvl, 1, 1)
    }
    if lvl < 0 { return x_row(r) }
    return bar_px(r, lvl, 2, 16) + bar_px(r, lvl, 3, 4) + bar_px(r, lvl, 4, 1)
}

/* Local display */
every 1s {
    let bars: int = csq_bars(sig, net_ok)
    if bars != bars_shown {
        lcd.create_char(1, cell_row(0, bars, 1), cell_row(1, bars, 1), cell_row(2, bars, 1), cell_row(3, bars, 1), cell_row(4, bars, 1), cell_row(5, bars, 1), cell_row(6, bars, 1), cell_row(7, bars, 1))
        lcd.create_char(2, cell_row(0, bars, 2), cell_row(1, bars, 2), cell_row(2, bars, 2), cell_row(3, bars, 2), cell_row(4, bars, 2), cell_row(5, bars, 2), cell_row(6, bars, 2), cell_row(7, bars, 2))
        bars_shown = bars
    }

    lcd.set_cursor(0, 0)
    if door_open == 1 {
        lcd.print("Door OPEN     ")
    } else {
        lcd.print("Door closed   ")
    }
    lcd.write_char(1)
    lcd.write_char(2)
    lcd.set_cursor(0, 1)
    if alarm == Alarm.armed    { lcd.print("ARMED   v{shock}     ") }
    if alarm == Alarm.intrusion { lcd.print("!INTRUSION!     ") }
    if alarm == Alarm.tamper   { lcd.print("!TAMPER!        ") }
    if alarm == Alarm.cooldown { lcd.print("cooldown        ") }
}

every 60s {
    let heap: int = system.free_heap()
    log("health", "heap={heap} alerts={alert_count}")
}

setup {
    lcd.clear()
    lcd.print("cabinet-ids")
    log("boot", "cabinet-ids node, alerts so far: {alert_count}")
}
