var n: int = 0

var x: int = 0

setup {
    match(n) {
        0 -> x = 5
        1..3 -> log("a", "low")
        _ -> {
            x += 1
        }
    }
}
