
chip esp32
enum Color {
    red
    green
}
enum Size {
    small
    large
}
setup {
    var c: Color = Color.red
    if c == Size.small {
        log("x", "y")
    }
}
