
task pid {
    every 1ms strict {
        wifi.connect("a", "b")
    }
}
