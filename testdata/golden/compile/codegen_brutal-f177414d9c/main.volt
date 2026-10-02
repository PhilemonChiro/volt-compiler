chip esp32
hardware { pin led: Output = 4 }
var restrict = 0
every 1s { led.toggle() }
