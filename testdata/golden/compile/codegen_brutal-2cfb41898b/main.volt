chip esp32
hardware { pin led: Output = 4 }
var return = 0
every 1s { led.toggle() }
