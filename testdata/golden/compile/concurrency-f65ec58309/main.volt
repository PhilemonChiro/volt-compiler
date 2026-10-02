
chip esp32

hardware {
    pin led: Output = 2
}

var sole_var = 42

task solo {
    every 1second {
        sole_var = sole_var + 1
        led.toggle()
    }
}

setup {
    run(solo)
}
