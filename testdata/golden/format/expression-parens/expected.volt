var a: int = 1

var b: int = 2

var c: int = 3

var ok: bool = true

setup {
    let x: int = a * (b - c) / 4
    let y: int = a - (b - c)
    let z: int = (a + b) * c
    let w: int = a - b - c
    let v: int = -(a + b)
    let t: bool = !(ok && a > b)
    let u: bool = (ok || a > b) && b > c
    let f: float = float(255 * (500 - a)) / 500.0
    log("x", "{x} {y} {z} {w} {v} {t} {u} {f}")
}
