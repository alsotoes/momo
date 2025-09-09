package common

// https://programming.guide/go/find-search-contains-slice.html
func Contains(a []string, x string) int {
        for index, n := range a {
                if x == n {
                        return index
                }
        }
        return -1
}
