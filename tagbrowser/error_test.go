// errortest.go
package tagbrowser

type fingerPrint []int
type searchPrint struct {
	wanted   fingerPrint
	unwanted fingerPrint
}

func errTest() searchPrint {
	frags := map[int]int{}
	searchP := searchPrint{}
	for k, v := range frags {
		if v > 0 {
			searchP.wanted = append(searchP, k)
		} else {
			searchP.unwanted = append(searchP, k)
		}
	}
	return searchP
}

func main() {
	errTest()
}
