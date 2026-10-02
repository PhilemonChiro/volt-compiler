chip esp32
/* This is a
   multi-line comment */
hardware {
    pin led: Output = 2
}
setup {
    /* inline */ led.on()
}
