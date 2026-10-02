
chip esp32
hardware { pin led: Output = 2 }
setup {
    for i in 0..100 step 5 {
        led.toggle()
    }
}
