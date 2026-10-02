
chip esp32
storage boot_count: int = 0

setup {
    boot_count = boot_count + 1
    storage.save("boot_count", boot_count)
}
