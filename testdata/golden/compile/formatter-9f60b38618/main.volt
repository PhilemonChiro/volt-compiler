
chip esp32
hardware{pin led:Output=2}
task worker{
every 1s{
led.toggle() or skip
led.on() or retry(3)
led.off() or restart
}
}
