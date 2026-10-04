var x: int = 0

machine m {
    state a {
        after 1s -> b { x = 1 }
    }

    state b {
        on x == 1 [x > 0] -> a {
            x = 0
            log("m", "back to a")
        }
    }
}
