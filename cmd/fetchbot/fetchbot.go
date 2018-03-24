package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/donomii/tagdb/tagbrowser"

	"golang.org/x/net/html"

	"github.com/PuerkitoBio/fetchbot"
	"github.com/PuerkitoBio/purell"
)

func dispatchURLs(urlCh chan string, matchString string) {
	var r *regexp.Regexp
	if matchString != "" {
		r = regexp.MustCompile(matchString)
	}

	f := fetchbot.New(fetchbot.HandlerFunc(handler))
	queue := f.Start()
	seenURL := map[string]bool{}
	for rawUrl := range urlCh {
		url, _ := purell.NormalizeURLString(rawUrl, purell.FlagRemoveDotSegments|purell.FlagDecodeDWORDHost|purell.FlagDecodeOctalHost|purell.FlagDecodeHexHost)
		if !seenURL[url] {
			seenURL[url] = true
			if r != nil && r.MatchString(url) {
				if debug {
					log.Printf("Enqueing for download: %s\n", url)
				}
				queue.SendStringGet(url)
			}
		}
	}
	queue.Close()
}

var urlCh chan string
var rpcClient *rpc.Client
var debug = false

func hasSymbol(str string) bool {
	for _, letter := range str {
		if unicode.IsSymbol(letter) {
			return true
		}
	}
	return false
}

func main() {
	var matchString string
	flag.StringVar(&tagbrowser.ServerAddress, "server", tagbrowser.ServerAddress, fmt.Sprintf("Server IP and Port.  Default: %s", tagbrowser.ServerAddress))
	flag.BoolVar(&debug, "debug", false, "Print extra debugging information")
	flag.StringVar(&matchString, "match", matchString, "Only follow URLs that match this regular expression")
	flag.Parse()
	urls := flag.Args()
	if debug {
		log.Println("Debugging active")
	}
	var err error
	rpcClient, err = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
	if err != nil {
		log.Printf("Failed to connect to tagserver on %s, exiting\n", tagbrowser.ServerAddress)
		os.Exit(1)
	} else {

		log.Printf("Connected to tagserver on %s\n", tagbrowser.ServerAddress)
	}
	urlCh = make(chan string)

	go dispatchURLs(urlCh, matchString)
	for _, url := range urls {
		if debug {
			log.Printf("Starting url: %s\n", url)
		}
		urlCh <- url
	}

	for {
		time.Sleep(1 * time.Second)
	}
}

func handler(ctx *fetchbot.Context, res *http.Response, err error) {
	if err != nil {
		fmt.Printf("error: %s\n", err)
		return
	}

	if debug {
		log.Println("Processing page: ", ctx.Cmd.URL())
	}

	defer res.Body.Close()
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Failed to process url, because %v\n", r)
		}
	}()
	body, err := ioutil.ReadAll(res.Body)
	if !utf8.Valid(body) {
		log.Println("Rejecting ", ctx.Cmd.URL(), " due to invalid unicode characters")
	} else {
		log.Printf("[%d] %s %s\n", res.StatusCode, ctx.Cmd.Method(), ctx.Cmd.URL())

		//fmt.Println(string(body))
		body = []byte(RemoveTags(string(body)))
		f := tagbrowser.RegSplit(strings.ToLower(string(body)), tagbrowser.FragsRegex)

		filtered := []string{}
		for _, x := range f {
			if !hasSymbol(x) {
				filtered = append(filtered, x)
			}
		}

		args := &tagbrowser.InsertArgs{fmt.Sprintf("%s", ctx.Cmd.URL()), -1, filtered}
		reply := &tagbrowser.SuccessReply{}
		rpcClient, err = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		if err != nil {
			log.Printf("Failed to connect to %s, exiting\n", tagbrowser.ServerAddress)
			return
		} else {

			log.Printf("Connected to tagserver on %s\n", tagbrowser.ServerAddress)
		}
		rpcClient.Call("TagResponder.InsertRecord", args, reply)

		var (
			anchorTag = []byte{'a'}
			hrefTag   = []byte("href")
			//httpTag   = []byte("http")
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
						//fmt.Printf("%s, %s\n", key, val)
						parentURL := ctx.Cmd.URL()
						href, _ := url.Parse(fmt.Sprintf("%s", val))
						//log.Printf("Found href: %s\n", href)

						urlCh <- fmt.Sprintf("%s", parentURL.ResolveReference(href))
					}

				}
			}
		}
	}
	log.Printf("Finished %s\n", ctx.Cmd.URL())

}

func RemoveTags(body string) string {
	r := regexp.MustCompile(`<.*?>`)
	unTaggedBody := r.ReplaceAllString(body, "")
	return unTaggedBody
}
