chip esp32
hardware { pin led: Output = 4 }
var static = 0
every 1s { led.toggle() }
