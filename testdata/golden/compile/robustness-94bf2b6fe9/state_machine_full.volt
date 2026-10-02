
chip esp32
hardware {
    pin led: Output = 2
}
machine traffic {
    initial state green {
        enter {
            led.on()
        }
        exit {
            led.off()
        }
        after 5s -> yellow
    }
    state yellow {
        enter {
            led.on()
        }
        after 2s -> red
    }
    state red {
        enter {
            led.off()
        }
        after 5s -> green
    }
}
