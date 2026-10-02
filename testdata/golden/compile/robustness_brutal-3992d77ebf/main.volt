chip esp32
hardware { pin led: Output = 2 }
state_machine door {
    state closed {
        on event { -> totally_made_up_state }
    }
}
every 1s { led.toggle() }
