package main

import (
	"bytes"
	"donomii/tagbrowser"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/PuerkitoBio/fetchbot"
)

func dispatchURLs(urlCh chan string) {
	f := fetchbot.New(fetchbot.HandlerFunc(handler))
	queue := f.Start()
	seenURL := map[string]bool{}
	for url := range urlCh {
		if !seenURL[url] {
			seenURL[url] = true
			queue.SendStringGet(url)
		}
	}
	queue.Close()
}

var urlCh chan string
var rpcClient *rpc.Client

func main() {
	rpcClient, _ = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
	urlCh = make(chan string)
	go dispatchURLs(urlCh)
	urlCh <- "http://msn.com"

	for {
		time.Sleep(1 * time.Second)
	}
}

func handler(ctx *fetchbot.Context, res *http.Response, err error) {
	if err != nil {
		fmt.Printf("error: %s\n", err)
		return
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	fmt.Printf("[%d] %s %s\n", res.StatusCode, ctx.Cmd.Method(), ctx.Cmd.URL())

	//fmt.Println(string(body))
	f := tagbrowser.RegSplit(strings.ToLower(string(body)), tagbrowser.FragsRegex)

	args := &tagbrowser.InsertArgs{fmt.Sprintf("%s", ctx.Cmd.URL()), -1, f}
	reply := &tagbrowser.SuccessReply{}

	rpcClient.Call("TagResponder.InsertRecord", args, reply)

	var (
		anchorTag = []byte{'a'}
		hrefTag   = []byte("href")
		httpTag   = []byte("http")
	)

	bodyR := bytes.NewReader(body)
	//defer bodyR.Close()
	tkzer := html.NewTokenizer(bodyR)

	for {
		switch tkzer.Next() {
		case html.ErrorToken:
			// HANDLE ERROR
			return

		case html.StartTagToken:
			tag, hasAttr := tkzer.TagName()
			if hasAttr && bytes.Equal(anchorTag, tag) { // a
				// HANDLE ANCHOR
				key, val, _ := tkzer.TagAttr()
				if bytes.Equal(hrefTag, key) { // href, http(s)
					// HREF TAG
					fmt.Printf("%s, %s\n", key, val)
					if bytes.HasPrefix(val, httpTag) {

						urlCh <- fmt.Sprintf("%s", val)
					} else {
						urlCh <- fmt.Sprintf("%s/%s", ctx.Cmd.URL(), val)
					}
				}

			}
		}
	}

}
