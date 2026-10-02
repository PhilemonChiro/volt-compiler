chip esp32
hardware { pin led: Output = 4 }
task t {
  led.on()
  sleep(100ms)
  led.off()
}
