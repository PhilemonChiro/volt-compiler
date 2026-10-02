chip esp32
hardware { pin led: Output = 4 }
channel q: int[100000]
every 1s { led.toggle() }
