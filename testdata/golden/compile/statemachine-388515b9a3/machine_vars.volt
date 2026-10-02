
machine Counter {
    var count = 0
    var max_count = 10

    initial state counting {
        after 1seconds -> counting
    }
}
