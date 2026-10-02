
enum State { idle, active }
hardware { pin led = 2 }
var s: State = State.idle
every 1s when s == State.active {
    led.toggle()
}
