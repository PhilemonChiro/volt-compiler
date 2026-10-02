
chip esp32

hardware {
    pin led: Output = 2
    pin button: PullUpInput = 0
}

machine DeviceState {
    var retry_count = 0

    initial state idle {
        enter {
            led.off()
        }
        on button.press() -> connecting
    }

    state connecting {
        enter {
            retry_count = retry_count + 1
            led.on()
        }
        on wifi.connected() -> online
        on wifi.disconnected() [retry_count < 5] -> connecting
        on wifi.disconnected() -> error
        after 10seconds -> error
    }

    state online {
        enter {
            led.on()
        }
        exit {
            led.off()
        }
        on wifi.disconnected() -> connecting {
            retry_count = 0
        }
    }

    state error {
        enter {
            led.off()
        }
        after 30seconds -> idle {
            retry_count = 0
        }
    }
}
