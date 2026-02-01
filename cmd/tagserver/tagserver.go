// tagserver.go
package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/donomii/tagdb/tagbrowser"
)

func main() {
	runtime.GOMAXPROCS(20) // runtime.GOMAXPROCS(2)
	manor := tagbrowser.StartServer()

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	log.Println("Server started. waiting for signal")
	<-stop
	log.Println("Shutting down...")
	manor.Shutdown()
	log.Println("Shutdown complete.")
	os.Exit(0)
}
