
hardware { pin btn: PullUpInput = 0 }
setup {
    var p: bool = btn.is_pressed()
}
