// status.go
package main

import (
	"donomii/tagbrowser"
	"flag"
	"fmt"
	"log"
	"net/rpc/jsonrpc"
	"strings"
)

func search(terms []string, displayFingerprint bool) {

	log.Println("Searching for", terms)
	client, err := jsonrpc.Dial("tcp", tagbrowser.ServerAddress)

	if err != nil {
		log.Fatal("dialing:", err)
	}

	searchTerm := strings.Join(terms, " ")
	args := &tagbrowser.Args{searchTerm, 10}
	preply := &tagbrowser.Reply{}
	err = client.Call("TagResponder.SearchString", args, preply)
	if err != nil {
		log.Println("RPC error:", err)
	}
	for _, v := range preply.C {
		if displayFingerprint {
			fmt.Printf("%v: %v(%v) %v\n", v.Score, v.Filename, v.Line, v.Fingerprint)
		} else {
			fmt.Printf("%v: %v(%v)\n", v.Score, v.Filename, v.Line)
		}
	}
	log.Println("Search complete")
}

func status() {
	log.Println("Checking tag database status")
	client, err := jsonrpc.Dial("tcp", tagbrowser.ServerAddress)

	if err != nil {
		log.Fatal("dialing:", err)
	}
	args := &tagbrowser.Args{"", 0}
	sreply := &tagbrowser.StatusReply{}
	log.Println("Fetching status")
	err = client.Call("TagResponder.Status", args, sreply)
	log.Println("Received status")
	if err != nil {
		log.Fatal("RPC error:", err)
	}
	fmt.Println("Status: ", *sreply)

	hreply := &tagbrowser.HistoReply{}
	log.Println("Fetching status")
	err = client.Call("TagResponder.HistoStatus", args, hreply)
	log.Println("Received status")
	if err != nil {
		log.Fatal("RPC error:", err)
	}
	fmt.Println("Status: ", hreply)
	fmt.Printf("0: %v\n1: %v\n2: %v\n3: %v\n4: %v\n5: %v\n6: %v\n7: %v\n8: %v\n", hreply.TagsToFilesHisto["0"], hreply.TagsToFilesHisto["1"], hreply.TagsToFilesHisto["2"], hreply.TagsToFilesHisto["3"], hreply.TagsToFilesHisto["4"], hreply.TagsToFilesHisto["5"], hreply.TagsToFilesHisto["6"], hreply.TagsToFilesHisto["7"], hreply.TagsToFilesHisto["8"])

	treply := &tagbrowser.TopTagsReply{}
	log.Println("Fetching status")
	err = client.Call("TagResponder.TopTagsStatus", args, treply)
	log.Println("Received status")
	if err != nil {
		log.Fatal("RPC error:", err)
	}
	fmt.Println("Status: ", *treply)

	log.Println("Check complete")
}

var completeMatch = false

func main() {
	fetchStatus := false
	displayFingerprint := false
	flag.BoolVar(&completeMatch, "completeMatch", false, "Do not return partial matches")
	flag.BoolVar(&fetchStatus, "status", false, "Report status")
	flag.BoolVar(&displayFingerprint, "fingerprint", false, "Display the tag fingerprint for each result")
	flag.Parse()
	terms := flag.Args()
	if len(terms) < 1 {
		fmt.Println("Use: query.exe  < --completeMatch >  search terms")

	}

	if fetchStatus {
		status()
	} else {
		search(terms, displayFingerprint)
	}
}
