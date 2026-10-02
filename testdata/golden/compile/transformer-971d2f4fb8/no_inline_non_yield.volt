
chip esp32
hardware { pin led: Output = 2 }
func helper(n: int) -> int {
    return n + 1
}
setup {
    var x: int = helper(5)
}
