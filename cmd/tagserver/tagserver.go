// tagserver.go
package main

import (
"github.com/donomii/tagdb/tagbrowser"
	"net/http"
	_ "net/http/pprof"
	"time"
	"runtime"
)

func main() {
	runtime.GOMAXPROCS(20) // runtime.GOMAXPROCS(2) 
	tagbrowser.StartServer()
	http.ListenAndServe("localhost:6060", nil)
	for {
		time.Sleep(100.0 * time.Millisecond)
		//log.Println("Blup")
	}
}
