chip esp32
hardware { pin led: Output = 2 }
let x: int = "hello"
every 1s { led.toggle() }
