cd ../release || exit 1
go build ../cmd/tagshell/ && echo Built text UI
go build ../cmd/tagquery/ && echo Built command line query tool
go build ../cmd/tagserver/ && echo Built tag server
go build ../cmd/tagloader/ && echo Built command line loader
go build ../cmd/fetchbot/ && echo Built command line http loader
