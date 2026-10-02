chip esp32
hardware { pin led: Output = 4 }
every 60s { led.toggle() }
