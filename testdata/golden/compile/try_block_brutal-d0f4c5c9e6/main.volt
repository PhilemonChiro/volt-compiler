chip esp32
hardware { pin led: Output = 4 }
setup { try { wifi.connect("a","b") } or retry 5 times else restart }
every 1s { led.toggle() }
