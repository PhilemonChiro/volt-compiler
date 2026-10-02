chip esp32
hardware { pin led = 4 }
every 1s { led.toggle() }
