
chip rp2040
hardware { pin led: Output = 25 }
storage boot_count: int = 0
setup {
    boot_count = boot_count + 1
    storage.save("boots", boot_count)
}
