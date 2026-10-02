chip esp32
hardware { pin led: Output = 4 }
every 0ms { led.toggle() }
