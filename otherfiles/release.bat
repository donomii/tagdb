cd ..\release || exit 1
mkdir tagdb
mkdir tagdb\webfiles
copy README.md tagdb
copy webfiles tagdb\webfiles\
copy tagdb.conf tagdb
cd tagdb
go build ../../cmd/tagshell/
go build ../../cmd/tagquery/
go build ../../cmd/tagserver/
go build ../../cmd/tagloader/
go build ../../cmd/fetchbot/
