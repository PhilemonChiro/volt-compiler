chip esp32
hardware { pin led: Output = 4 }
setup { try { } or retry }
every 1s { led.toggle() }
