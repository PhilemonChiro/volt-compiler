
chip esp32
func clamp(val: int, lo: int, hi: int) -> int {
    if val < lo { return lo }
    if val > hi { return hi }
    return val
}
setup {
    var x: int = clamp(150, 0, 100)
}
