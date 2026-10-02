
hardware {
    pin led: Output = 2
}
machine DevState {
    initial state idle {
        on activate() -> running
    }
    state running {
        on deactivate() -> idle
    }
}
task blink {
    every 500ms {
        led.toggle()
    }
}
setup {
    run(blink)
}
