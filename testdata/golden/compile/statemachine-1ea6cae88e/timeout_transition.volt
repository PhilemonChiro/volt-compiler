
machine Timer {
    initial state waiting {
        after 5seconds -> done
    }
    state done {
        after 1seconds -> waiting
    }
}
