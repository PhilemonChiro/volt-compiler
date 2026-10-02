chip esp32
hardware { pin led: Output = 4 }
func f() { led.toggle() }
func f() { led.toggle() }
every 1s { f() }
