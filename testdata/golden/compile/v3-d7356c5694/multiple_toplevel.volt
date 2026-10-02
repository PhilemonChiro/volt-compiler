
hardware { pin led = 2 }
every 100ms { led.toggle() }
every 5s { log("health", "alive") }
every 30s { log("stats", "ok") }
