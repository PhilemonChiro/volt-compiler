
machine Conn {
    var attempts = 0

    initial state disconnected {
        on connect_event() -> connected {
            attempts = 0
        }
    }
    state connected {
        on disconnect_event() -> disconnected {
            attempts = attempts + 1
        }
    }
}
