chip esp32
hardware { pin led: Output = 4 }
var msg: string = "hello %s %n attack"
every 1s {
  led.toggle()
  log("tag", msg)
}
