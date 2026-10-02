chip esp32
hardware { pin led: Output = 4 }
var inline = 0
every 1s { led.toggle() }
