chip esp32


hardware { pin led: Output = 2 }

every 1s { ghost.toggle() }
