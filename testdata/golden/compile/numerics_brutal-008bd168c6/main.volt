chip esp32
hardware { pin led: Output = 4 }
var x = 0xFF
every 1s { led.toggle() }
