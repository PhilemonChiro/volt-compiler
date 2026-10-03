enum Mode { idle, running }
enum Severity { info, critical }

var last_event: string = "boot"
var mode: Mode = Mode.idle
var crit_count: int = 0

func severity_of(code: int) -> Severity {
    if code >= 100 { return Severity.critical }
    return Severity.info
}

task worker {
    every 100ms {
        var running: int = 0
        running += 1
        mode = Mode.running
        last_event = "tick {running}"
        if severity_of(running) == Severity.critical { crit_count += 1 }
    }
}

task logger {
    every 1s {
        let seen: string = last_event
        last_event = "logged"
    }
}
