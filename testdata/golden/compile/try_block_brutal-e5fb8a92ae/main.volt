chip esp32
hardware { pin led: Output = 4 }
setup { try { wifi.connect("a","b") } or fail }
every 1s { led.toggle() }
