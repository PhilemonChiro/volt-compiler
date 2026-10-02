
hardware {
    pin led: Output = 2
    pin button: PullUpInput = 0
}

machine DeviceState {
    state idle {
        on button.press() -> running
    }
    state running {
        every 1second {
            led.toggle()
        }
    }
}
