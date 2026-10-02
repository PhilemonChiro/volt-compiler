chip stm32f103

hardware {
    pin led: Output = PC13
}

setup {
    wifi.connect("test", "pass")
}
