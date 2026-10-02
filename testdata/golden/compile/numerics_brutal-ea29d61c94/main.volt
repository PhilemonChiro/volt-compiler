chip esp32
hardware { pin led: Output = 4 }
var x = -42
every 1s { led.toggle() }
