# Volt language

Volt is a language for microcontroller firmware (ESP32 first; ESP8266 and
RP2040 too). A program declares its hardware, then describes behaviour as
periodic blocks, tasks, state machines and functions. The compiler checks
pins, buses and types against the chip, then emits C.

Every `volt` example below is a complete program that compiles cleanly.

## Rules that are easy to get wrong

- No semicolons. One statement per line. Blocks use `{ }`.
- The chip is set in `volt.toml` (`chip = "esp32"`), not in the source.
- `log` takes two arguments: a tag and a message, `log("tag", "msg")`.
- Strings interpolate with `{expr}`: `"temp={t}"`. `+` also concatenates a
  string with any value.
- Durations are literals with a unit: `100us`, `50ms`, `2s`, `1second`,
  `5seconds`, `10min`, `1hour`. A plain number is not a duration.
- Convert between number types explicitly: `int(x)`, `float(n)`.
- State-machine guards use square brackets: `on x > 3 [ready] -> next`.
- Identifiers must not start with `let`, `var` or `else` (`variance` lexes as
  `var iance`).
- `let` is immutable, `var` is mutable. Globals need an initial value.

## Program structure

```volt
hardware {
    pin led: Output = 2
}

setup {
    log("boot", "ready")
}

var count: int = 0

func double(x: int) -> int {
    return x * 2
}

every 500ms {
    led.toggle()
    count = double(count) + 1
}
```

Top-level items: `include`, `hardware`, `enum`, `struct`, `storage`,
`channel`, global `let`/`var`, `func`, `every` (periodic block), `task`,
`machine`, and `setup` (runs once at boot). Write `setup` right after `hardware`; the formatter puts it there.

## Hardware

Pins declare a mode and a GPIO number. The mode can be omitted when usage
makes it clear.

```volt
hardware {
    pin led = 2
    pin relay: Output = 4
    pin button: PullUpInput = 15
    pin pot: Analog = 34
    pin buzzer: PWM = 18
    pin arm: Servo = 13
}

every 100ms {
    if button.is_pressed() {
        relay.on()
        buzzer.set_pwm(128)
    } else {
        relay.off()
        buzzer.set_pwm(0)
    }
    arm.move(pot.read() * 180 / 4095)
    led.toggle()
}
```

Devices come from the device library (listed in the reference below). The
short form lets the compiler pick the bus, pins and default address:

```volt
hardware {
    device env: BME280
}

every 2s {
    let t: float = env.temperature()
    log("env", "temp={t}")
}
```

To control wiring, declare the bus and attach devices with `on` and `at`:

```volt
hardware {
    device lcd: LCD16x2 on i2c0 at 0x27
    device accel: ADXL345 on i2c0 at 0x53
    device gsm: SIM800L on modem_uart
    i2c i2c0(sda: 21, scl: 22)
    uart modem_uart(tx: 17, rx: 16, baud: 9600)
}

every 1s {
    lcd.set_cursor(0, 0)
    lcd.print("g={accel.magnitude()}")
}
```

Devices wired to GPIOs take their pins (and settings) as named arguments:

```volt
hardware {
    device sonar: HCSR04(trig: 26, echo: 27)
    device strip: NeoPixel(pin: 32, leds: 30)
}

every 200ms {
    let cm: float = sonar.distance_cm()
    log("sonar", "cm={cm}")
}
```

## Types and variables

Types: `int` (32-bit), `float`, `bool`, `string`, `byte`, enums, structs,
`Duration`.

```volt
enum Mode { idle, running, fault }

storage boots: int = 0

let limit: int = 100

var mode: Mode = Mode.idle

var ratio: float = 0.5

var armed: bool = false

setup {
    boots += 1
    storage.save("boots", boots)
    let half: int = int(float(limit) * ratio)
    log("boot", "boot #{boots}, half={half}")
    if mode == Mode.idle && !armed {
        mode = Mode.running
    }
}
```

`storage` variables persist across reboots once saved with `storage.save`.

Strings join with `+` (numbers and bools convert; results are cut at 127
characters). Built-ins: `len(s)`, `contains(s, part)`, `starts_with(s, prefix)`,
`ends_with(s, suffix)`, `to_int(s)`, `to_float(s)`; compare with `==` / `!=`.

```volt
setup {
    let line: string = "set 42"
    if starts_with(line, "set ") {
        let n: int = to_int("42")
        log("cmd", "value " + n + " from " + len(line) + " chars")
    }
}
```

## Control flow

```volt
hardware {
    pin led: Output = 2
}

setup {
    var n: int = 0
    while n < 3 { n += 1 }
    for i in 0..10 { log("loop", "i={i}") }
    for j in 0..100 step 10 { log("loop", "j={j}") }
    if n > 2 {
        led.on()
    } else if n == 2 {
        led.toggle()
    } else {
        led.off()
    }
    match(mode) {
        .idle -> led.off()
        .running -> led.on()
        _ -> led.toggle()
    }
}

enum Mode { idle, running, fault }

var mode: Mode = Mode.idle
```

`a..b` excludes `b`, in `for` loops and in `match` ranges. `match` arms are `.variant`, an int, an int
range `a..b`, or `_`. `break`, `continue` and `return` work as usual.

## Periodic blocks and tasks

`every` runs a block on a period. `when` skips a run while its condition is
false. `strict` runs it from a hardware timer (keep it short, no blocking
calls). A `task` groups local state with its periodic blocks.

```volt
hardware {
    pin led: Output = 2
    pin motor: PWM = 4
    pin button: PullUpInput = 15
}

var presses: int = 0

every 1s { led.toggle() }

every 10s when wifi.is_connected() {
    mqtt.send_int("presses", presses)
}

every 1ms strict { motor.set_pwm(100) }

task buttons {
    var last: int = 1
    every 20ms {
        let now: int = button.read()
        if now == 0 && last == 1 {
            presses += 1
        }
        last = now
    }
}
```

Tasks are scheduled automatically: plain tasks share a cooperative
scheduler; a task that calls blocking APIs (WiFi, MQTT, HTTP, modems) gets
its own RTOS task; `strict` blocks run from a timer interrupt.

## State machines

States have optional `enter` and `exit` blocks and transitions:
`on <condition> -> target`, `after <duration> -> target`, each with an
optional `[guard]` and an optional action block. The first state (or the
one marked `initial`) is where the machine starts.

```volt
hardware {
    pin relay: Output = 4
    pin button: PullUpInput = 15
}

var request: bool = false

var failures: int = 0

every 50ms {
    if button.is_pressed() {
        request = true
    }
}

machine door {
    initial state locked {
        enter { relay.off() }
        on request [failures < 3] -> unlocked { request = false }
        on failures >= 3 -> alarm
    }

    state unlocked {
        enter { relay.on() }
        exit { log("door", "relocking") }
        after 10s -> locked
    }

    state alarm {
        enter { log("door", "ALARM") }
        after 30s -> locked { failures = 0 }
    }
}
```

## Channels

Channels pass values between tasks. `int[8]` gives a buffer of 8.

```volt
channel events: int[8]

every 1s { events.send(1) }

task consumer {
    every 100ms {
        if events.has_data() {
            let e: int = events.receive()
            log("event", "got {e}")
        }
    }
}
```

## Failure handling

A call that can fail takes an `or` fallback: `or retry(n)`, `or skip`,
`or restart`, or a default value. A `try` block takes `or retry`, `or skip`
or `or restart`.

```volt
hardware {
    device env: BME280
}

setup {
    wifi.connect("ssid", "password") or retry(5)
    mqtt.connect("mqtt://broker.local") or retry(3)
}

every 10s {
    try {
        let t: float = env.temperature()
        mqtt.send_float("home/temp", t)
    } or skip
}
```

## Networking

`wifi`, `mqtt` and `http` are built in. `http.get(url)` and
`http.post(url, body)` return the response body as a string (empty on
failure, cut at 1023 characters). `mqtt.on_receive(topic)` subscribes; the
latest message is read with `mqtt.has_message()`, `mqtt.payload()` (marks it
read) and `mqtt.topic()`.

```volt
on mqtt.has_message() {
    let cmd: string = mqtt.payload()
    log("mqtt", mqtt.topic() + ": " + cmd)
}

every 60s when wifi.is_connected() {
    let body: string = http.get("http://example.com/status")
    log("http", body)
}

setup {
    wifi.connect("ssid", "password") or retry(5)
    mqtt.connect("mqtt://broker.local") or retry(3)
    mqtt.on_receive("home/cmd")
}
```
