package main

import (
	"log"

	_ "github.com/BurntSushi/toml"
	_ "github.com/PuerkitoBio/fetchbot"
	_ "github.com/PuerkitoBio/purell"
	_ "github.com/cornelk/hashmap"
	_ "github.com/nsf/termbox-go"
	_ "github.com/skratchdot/open-golang/open"
	_ "github.com/ungerik/go-dry"
)

func main() {
	log.Println(`This file only exists to force go to download all the prerequisite modules needed to compile all the subprograms.
    
	    Please run the following commands to compile tagdb:
    
	    go build cmd/tagshell/tagshell.go
	    go build cmd/tagquery/tagquery.go
	    go build cmd/tagserver/tagserver.go
	    go build cmd/tagloader/tagloader.go
	    `)
	os.Exit(1)
}
