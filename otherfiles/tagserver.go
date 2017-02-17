// tagserver.go
package main

import (
	"donomii/tagbrowser"
	"net/http"
	_ "net/http/pprof"
	"time"
)

func main() {
	tagbrowser.StartServer()
	http.ListenAndServe("localhost:6060", nil)
	for i := 0; i < 1; i = 0 {
		time.Sleep(1 * time.Second)
	}
}
