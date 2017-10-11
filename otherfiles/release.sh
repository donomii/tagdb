#!/bin/sh
cd ../release || exit 1
mkdir tagdb
cp -r webfiles tagdb
cp README.md tagdb
cp tagdb.conf tagdb
cd tagdb
go build ../../cmd/tagshell/ && echo Built text UI
go build ../../cmd/tagquery/ && echo Built command line query tool
go build ../../cmd/tagserver/ && echo Built tag server
go build ../../cmd/tagloader/ && echo Built command line loader
go build ../../cmd/fetchbot/ && echo Built command line http loader
