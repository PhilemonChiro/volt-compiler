chip esp32
hardware { pin led: Output = 4 }
enum mode { idle, run, stop }
var m: mode = mode.idle
every 1s { led.toggle() }
