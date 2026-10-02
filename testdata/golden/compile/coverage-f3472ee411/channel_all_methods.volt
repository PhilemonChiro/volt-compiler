
chip esp32
channel q: int[8]

task t {
    every 1s {
        q.send(1)
        var x: int = q.receive()
        var n: int = q.count()
        var empty: bool = q.is_empty()
        var full: bool = q.is_full()
        q.clear()
    }
}
