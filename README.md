[![Build Status](https://travis-ci.org/donomii/tagdb.svg?branch=master)](https://travis-ci.org/donomii/tagdb)
[![GoDoc](https://godoc.org/github.com/donomii/tagdb?status.svg)](https://godoc.org/github.com/donomii/tagdb)

# tagdb

Tagdb is a text search engine that offers fast word completion, and real time searches.

Unfortunately, the loading process is quite slow right now.

Tagdb stores urls and tags, allowing you to index files, webpages, and anything else you can reasonably access via a url.  It can also store line numbers for a file, allowing you to jump straight to your search result.

## Installation

### Build

    go build -o release/tagshell cmd/tagshell/tagshell.go
    go build -o release/tagquery cmd/tagquery/tagquery.go 
    go build -o release/tagserver cmd/tagserver/tagserver.go
    go build -o release/tagloader cmd/tagloader/tagloader.go

### Start

then start tagserver

    ./tagserver &

then load some files

    ./tagloader -verbose .

then run a search with

    ./tagquery quick brown fox

and you will see

    ./query quick brown fox
    2017/04/06 18:47:01 Searching for [quick brown fox]
    3: otherfiles/testsearch.txt(1)
    3: README.md(29)
    2017/04/06 18:47:01 Search complete

## Use


### tagshell

tagshell is a simple command line GUI that uses predictive, real time search to list your results and jump to them.

Start typing your search until you see the results you want, then press the down arrow to select the result you want to examine.  Then right arrow will open that file.
    
### tagloader

tagloader recursively scans files and directories, indexing their contents

      -addRecord
            Add record from the command line
      -debug
            Display additional debug information
      -noContents
            Do not look inside files
      -parallel int
            Maximum number of simultaneous inserts to attempt (default 1)
      -server string
            Server IP and Port.  Default: 127.0.0.1:6781 (default "127.0.0.1:6781")
      -verbose
            Show files as they are loaded

-verbose will print every filename as it is scanned.

By default, tagloader will treat the entire contents of the file as one "search result".  It reads the entire file, building a tag list, and then stores that list.  There are two options to control this:

-noContents will ignore the file contents and only store the file path (split up by usual word boundaries).  Searches will only return a file if your search word occurs in the file name.  -noContents is handy for indexing things like mp3 collections and photographs, where the contents contain no text.

-everyLine will store every line in a text file separately, so search results can return multiple lines in the same file.  You can then jump to the correct line using programs like tagshell.

Tagloader creates a record in the database using on the path to the file (based on the command line argument).  It does no further processing of the path, and won't even normalise it.  So if you give it a relative path, it will store relative paths, which will make it difficult to find the file again if you search for it while in another directory.

Relative paths are useful for things like indexing a webserver directory, so you can later build a full URL from the relative path and the server name.  Absolute paths are more useful if you plan to access the files from the command line or other programs.


### tagquery

tagquery searches the database, and can also command the database to shutdown

      -completeMatch
            Do not return partial matches
      -fingerprint
            Display the tag fingerprint for each result
      -server string
            Server IP and Port.  Default: 127.0.0.1:6781 (default "127.0.0.1:6781")
      -shutdown
            Shutdown the server
      -status
            Report status

#### -completeMatch

By default, tagdb shows you partial matches.  If a record matches some of the tags you provided, it will be returned (with a lower score than if you matched all the tags).  This is slower and clutters up the results, so you can request -completeMatch.  -completeMatch will only return records where all your search terms match all the tags for the record.

#### -shutdown

Order the server to quit.  This will take several seconds or minutes, depending on which storage layer you chose for your data.

#### -status

Print some server statistics


### tagserver

tagserver is the main database, which listens for JSON-RPC requests and servers answers

      -config string
            Config file to load settings from (default "tagdb.conf")
      -cpuprofile string
            write cpu profile to file
      -debug
            Print extra debugging information.  Default: false
      -preAlloc int
            Allocate this many entries at startup.  Default: 1000000 (default 1000000)

#### -config

Read a different configuration file.  The default file is "tagdb.conf", in the current directory.

#### -preAlloc

If the database files run out of room, they must be extended and this takes some time.  Preallocating entries can speed up this process.

### fetchbot

fetchbot crawls a website and adds it to the database

      -match string
            Only follow URLs that match this regular expression
      -server string
            Server IP and Port.  Default: 127.0.0.1:6781 (default "127.0.0.1:6781")

Example:

    ./fetchbot --match "rock" -debug https://www.rockpapershotgun.com/

