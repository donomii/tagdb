cd ..\release || exit 1
go build ../cmd/tagshell/
go build ../cmd/tagquery/
go build ../cmd/tagserver/
go build ../cmd/tagloader/
go build ../cmd/fetchbot/