
machine Monitor {
    initial state running {
        on stop() -> stopped
    }
    state stopped {
        on start() -> running
    }
}
