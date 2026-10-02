chip esp32
hardware {
    pin led: Output = 2
    pin sig: Input = 4
    device w: DHT22 on sig at 4
}
every 1s {
  led.toggle()
  w.humidity()
}
