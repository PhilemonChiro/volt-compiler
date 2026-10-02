chip esp32
hardware { pin led: Output = 4 }
setup { try { wifi.connect("a","b") } or retry 2 times else fail }
every 1s { led.toggle() }
