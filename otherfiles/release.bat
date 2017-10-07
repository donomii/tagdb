cd ..\release || exit 1
mkdir tagdb
copy README.md tagdb
copy /r webfiles tagdb
copy tagdb.conf tagdb
cd tagdb
go build ../../cmd/tagshell/
go build ../../cmd/tagquery/
go build ../../cmd/tagserver/
go build ../../cmd/tagloader/
go build ../../cmd/fetchbot/
