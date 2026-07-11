package pkg1
func ComplexPrivate(a, b int) {
    if a > 0 {
        if b > 0 {
            for i := 0; i < 10; i++ {
                if i % 2 == 0 {
                    _ = i
                } else {
                    _ = i + 1
                }
            }
        }
    }
    if a == 1 {}
    if a == 2 {}
    if a == 3 {}
    if a == 4 {}
    if a == 5 {}
}
