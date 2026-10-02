chip esp32
hardware { pin led: Output = 4 }
machine m {
    state a {
        enter { try { wifi.connect("a","b") } or retry 3 times else skip }
        after 1s -> b
    }
    state b { enter { led.on() } }
}
