
hardware {
    pin battery: Analog = 36
}
setup {
    let val = battery.read()
}
