
func clamp(val: int, lo: int, hi: int) -> int {
    if val < lo {
        return lo
    }
    if val > hi {
        return hi
    }
    return val
}
setup {
    let x = clamp(42, 0, 100)
}
