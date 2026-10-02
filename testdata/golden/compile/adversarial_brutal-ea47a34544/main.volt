chip esp32
hardware { pin led: Output = 4 }
var x = 0
var x = 1
var x = 2
every 1s { led.toggle() }
