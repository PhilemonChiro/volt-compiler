chip rp2040
hardware { pin led: Output = 25 }
every 1s { led.toggle() wifi.is_connected() }
