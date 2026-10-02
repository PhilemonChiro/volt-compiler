chip esp32
hardware {
    pin led: Output = 2
    pin sig: Input = 5
    device w: DHT11 on sig at 5
}
every 1s {
  led.toggle()
  w.temperature()
}
