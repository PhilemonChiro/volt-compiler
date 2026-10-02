
chip esp32
func max(a: int, b: int) -> int {
    if a > b {
        return a
    }
    return b
}

func min(a: int, b: int) -> int {
    if a < b {
        return a
    }
    return b
}

setup {
    var big: int = max(10, 20)
    var small: int = min(10, 20)
}
