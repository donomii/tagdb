// status.go
package main

import (
	"donomii/tagbrowser"
	"flag"
	"fmt"
	"log"
	"net/rpc/jsonrpc"
	"os"
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
	log.Println("General statistics and settings")
	log.Println("Status: ", *sreply)

	hreply := &tagbrowser.HistoReply{}
	log.Println("Fetching Histo Stats")
	err = client.Call("TagResponder.HistoStatus", args, hreply)
	log.Println("Received status")
	if err != nil {
		log.Fatal("RPC error:", err)
	}
	log.Println("Number of files the tag occurred in : Number of tags with this occurrence", hreply)
	for i := 0; i < 50; i = i + 1 {
		fmt.Printf("%d: %v\n", i, hreply.TagsToFilesHisto[fmt.Sprintf("%d", i)])
	}
	treply := &tagbrowser.TopTagsReply{}
	log.Println("Fetching status")
	err = client.Call("TagResponder.TopTagsStatus", args, treply)
	log.Println("Received status")
	if err != nil {
		log.Fatal("RPC error:", err)
	}
	fmt.Println("Top tags, by the number of files they occur in: \n")
	for k, v := range treply.TopTags {
		log.Println(k, ":", v)
	}
	log.Println("Check complete")
}

var completeMatch = false

func main() {
	var shutdown bool
	fetchStatus := false
	displayFingerprint := false
	flag.StringVar(&tagbrowser.ServerAddress, "server", tagbrowser.ServerAddress, fmt.Sprintf("Server IP and Port.  Default: %s", tagbrowser.ServerAddress))
	flag.BoolVar(&completeMatch, "completeMatch", false, "Do not return partial matches")
	flag.BoolVar(&fetchStatus, "status", false, "Report status")
	flag.BoolVar(&shutdown, "shutdown", false, "Shutdown the server")
	flag.BoolVar(&displayFingerprint, "fingerprint", false, "Display the tag fingerprint for each result")
	flag.Parse()
	if shutdown {
		client, _ := jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		args := &tagbrowser.Args{"", 0}
		sreply := &tagbrowser.StatusReply{}
		client.Call("TagResponder.Shutdown", args, sreply)
		os.Exit(0)
	}
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
