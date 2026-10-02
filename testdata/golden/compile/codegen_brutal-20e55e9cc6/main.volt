chip esp32
hardware { pin led: Output = 4 }
var if = 0
every 1s { led.toggle() }
