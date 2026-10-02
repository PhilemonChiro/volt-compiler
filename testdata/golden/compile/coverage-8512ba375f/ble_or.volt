
chip esp32
hardware { pin led: Output = 2 }
setup {
    ble.advertise("VoltDevice") or skip
}
