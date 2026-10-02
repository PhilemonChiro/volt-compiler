chip esp32
hardware { pin led: Output = 4 }
setup { try { let x: int = 1 } or retry }
every 1s { led.toggle() }
