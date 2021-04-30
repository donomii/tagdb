// status.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/donomii/goof"
)

func search(terms []string, displayFingerprint bool) {

	log.Println("Searching for", terms)
	if len(terms) > 0 {
		cmd := []string{"csearch", "-i", terms[0]}

		res := goof.QC(cmd)

		for _, t := range terms {
			res = goof.Grep(t, res)
		}

		lines := strings.Split(res, "\n")

		for _, v := range lines {
			fmt.Printf("%v\n", v)
		}
	}
	log.Println("Search complete")
}

var completeMatch = false

func main() {
	var shutdown bool
	fetchStatus := false
	displayFingerprint := false
	flag.BoolVar(&completeMatch, "completeMatch", false, "Do not return partial matches")
	flag.BoolVar(&fetchStatus, "status", false, "Report status")
	flag.BoolVar(&shutdown, "shutdown", false, "Shutdown the server")
	flag.BoolVar(&displayFingerprint, "fingerprint", false, "Display the tag fingerprint for each result")
	flag.Parse()
	if shutdown {
		os.Exit(0)
	}
	terms := flag.Args()
	if len(terms) < 1 {
		fmt.Println("Use: query.exe  < --completeMatch >  search terms")

	}

	search(terms, displayFingerprint)
}
