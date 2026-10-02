
machine A {
    initial state x {
        on go() -> y
    }
    state y {
        on back() -> x
    }
}
machine B {
    initial state p {
        on go() -> q
    }
    state q {
        on back() -> p
    }
}
