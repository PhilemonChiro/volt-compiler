hardware {
    device env: BME280
}

var last: float = 0.0

setup {
    let p: float = env.pressure() or restart
    last = env.temperature() or 20.0
}

every 10s {
    let t: float = env.temperature() or skip
    let h: float = env.humidity() or 50.0
    let body: string = http.get("http://example.com/cfg") or retry(3)
    last = env.temperature() or skip
    log("env", "{t} {h} {body} {last}")
}
