
deps:
	go get github.com/cornelk/hashmap
	go get github.com/skratchdot/open-golang/open
	go get github.com/ungerik/go-dry

build:
	go build -o release/tagshell cmd/tagshell/tagshell.go
	go build -o release/tagquery cmd/tagquery/tagquery.go
	go build -o release/tagserver cmd/tagserver/tagserver.go
	go build -o release/tagloader cmd/tagloader/tagloader.go
