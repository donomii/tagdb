package main

import (
	"github.com/ungerik/go-dry"
	"github.com/cornelk/hashmap"
	"github.com/BurntSushi/toml"
	"github.com/nsf/termbox-go"
	"github.com/PuerkitoBio/fetchbot"
	"github.com/skratchdot/open-golang/open"
	"github.com/PuerkitoBio/purell"
	  "log"
	    "net/http"
	    "os"
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
