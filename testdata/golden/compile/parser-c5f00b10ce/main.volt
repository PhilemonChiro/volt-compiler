
machine DeviceState {
    state idle {
        on button.press() -> connecting
    }
    state connecting {
        enter {
            led.blink(200ms)
        }
        on wifi.connected() -> running
        after 10seconds -> error
    }
    state running {
        on wifi.disconnected() -> connecting
    }
    state error {
        after 5seconds -> idle
    }
}
