// json_rpc_server.go

package tagbrowser

import "github.com/skratchdot/open-golang/open"
import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"runtime/pprof"
	"time"
)

//RPC

var shuttingDown bool = false

func (t *TagResponder) Shutdown(args *Args, reply *SuccessReply) error {
	shuttingDown = true
	defaultManor.Shutdown()
	reply.Success = true
	go func() {
		time.Sleep(time.Second * 5.0)
		os.Exit(0)
	}()
	return nil
}

func (t *TagResponder) Status(args *Args, reply *StatusReply) error {
	stats := map[string]string{}
	//stats["NumberOfRecords"] = fmt.Sprintf("%v", len(silo.database))
	//stats["InternedStrings"] = fmt.Sprintf("%v", silo.next_string_index+1)
	//stats["NumberOfAcceleratedTags"] = fmt.Sprintf("%v", len(silo.tag2file))
	//stats["DefaultSeparatorRegex"] = fmt.Sprintf("%v", BoundariesRegex)
	reply.Answer = stats
	//reply.TagsToFilesHisto, reply.TopTags = sumariseDatabase()
	log.Println("Status handler complete")
	return nil
}

func (t *TagResponder) HistoStatus(args *Args, reply *HistoReply) error {
	//reply.TagsToFilesHisto, _ = silo.sumariseDatabase()
	log.Println("Histo Status handler complete")
	return nil
}

func (t *TagResponder) TopTagsStatus(args *Args, reply *TopTagsReply) error {
	//_, reply.TopTags = silo.sumariseDatabase()
	log.Println("Top Tags Status handler complete")
	return nil
}

func (t *TagResponder) SearchString(args *Args, reply *Reply) error {

	log.Printf("Query: '%v'", args.A)
	if defaultManor != nil {
		res := defaultManor.scanFileDatabase(args.A, args.Limit, false)
		reply.C = res
	} else {
	}

	//searchF := silo.makeFingerprintFromSearch(args.A)
	//res := silo.scanFileDatabase(searchF, args.Limit, false)

	//log.Printf("Results: %v", res)
	//reply.C = []resultRecord(res)
	//for _, v := range res {
	//reply.C = append(reply.C, []string{fmt.Sprintf("%v", v.score), v.filename, fmt.Sprintf("%v", v.line), v.sample})

	//}

	log.Printf("Results: %d results for query '%v'", len(reply.C), args.A)

	return nil
}

func (t *TagResponder) PredictString(args *Args, reply *StringListReply) error {

	log.Printf("PredictString: '%v'", args.A)
	//res := defaultFarm.predictString(args.A, args.Limit)

	//log.Printf("Results: %v", res)
	//reply.C = []resultRecord(res)
	//for _, v := range res {
	//reply.C = append(reply.C, []string{fmt.Sprintf("%v", v.score), v.filename, fmt.Sprintf("%v", v.line), v.sample})

	//}
	//reply.C = res
	log.Printf("Results: %d results for predictString '%v'", len(reply.C), args.A)

	return nil
}

func (t *TagResponder) InsertRecord(args *InsertArgs, reply *SuccessReply) error {
    if debug {
        log.Println("Inserting record Handler", args)
    }
	//silo.LockMe()
	//defer func() { silo.UnlockMe() }()

	//f := makeFingerprint(args.Tags)
	if defaultManor != nil && !shuttingDown {
		rec := RecordTransmittable{args.Name, args.Position, args.Tags}
		defaultManor.SubmitRecord(rec)
		reply.Success = true
		reply.Reason = ""
	} else {
		if shuttingDown {
			reply.Success = false
			reply.Reason = "Server in shutdown mode"

		} else {
			reply.Success = false
			reply.Reason = "Server not ready"
		}
	}

    if debug {
        log.Println("Finished inserting record handler", reply)
    }
	return nil
}

func InsertRecord(args *InsertArgs, reply *SuccessReply) error {
    if debug {
        log.Println("Inserting record action", args)
    }
	rec := RecordTransmittable{args.Name, args.Position, args.Tags}
	defaultManor.SubmitRecord(rec)

	reply.Success = true
	reply.Reason = ""
    if debug {
        log.Println("Finished inserting record action", reply)
    }
	return nil
}

func (t *TagResponder) Error(args *Args, reply *Reply) error {
	log.Println("ERROR")
	panic("ERROR")
}

// rpcRequest represents a RPC request.
// rpcRequest implements the io.ReadWriteCloser interface.
type rpcRequest struct {
	r    io.Reader     // holds the JSON formated RPC request
	rw   io.ReadWriter // holds the JSON formated RPC response
	done chan bool     // signals then end of the RPC request
}

// NewRPCRequest returns a new rpcRequest.
func NewRPCRequest(r io.Reader) *rpcRequest {
	var buf bytes.Buffer
	done := make(chan bool)
	return &rpcRequest{r, &buf, done}
}

// Read implements the io.ReadWriteCloser Read method.
func (r *rpcRequest) Read(p []byte) (n int, err error) {
	return r.r.Read(p)
}

// Write implements the io.ReadWriteCloser Write method.
func (r *rpcRequest) Write(p []byte) (n int, err error) {
	r.done <- true
	return r.rw.Write(p)
}

// Close implements the io.ReadWriteCloser Close method.
func (r *rpcRequest) Close() error {
	return nil
}

// Call starts the RPC request, waits for it to complete, and returns the results.
func (r *rpcRequest) Call() io.Reader {
	if debug {
		log.Printf("Processing json rpc request\n")
	}
	arith := new(TagResponder)

	server := rpc.NewServer()
	server.Register(arith)

	//server.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)

	go server.ServeCodec(jsonrpc.NewServerCodec(r))
	//go jsonrpc.ServeConn(r)
	<-r.done
	//b := []byte{}
	//_, _ = r.rw.Read(b)
	log.Println("Returning")
	return r.rw
}

func rpc_server(serverAddress string) {
	arith := new(TagResponder)

	server := rpc.NewServer()
	server.Register(arith)

	//server.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)

	l, e := net.Listen("tcp", serverAddress)
	if e != nil {
		log.Fatal("listen error:", e)
	}

	http.HandleFunc("/rpc", func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		res := NewRPCRequest(req.Body).Call()
		io.Copy(w, res)
	})

	cwd, _ := os.Getwd()
	log.Printf("Serving /files/ from:%s on port 8181\n", cwd)

	http.Handle("/", http.FileServer(http.Dir("webfiles")))
	//FIXME
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(cwd))))

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	go http.ListenAndServe(":8181", nil)
	open.Start("http://localhost:8181/index.html")



	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		if debug {
		    log.Println("Got connection")
		}
		go server.ServeCodec(jsonrpc.NewServerCodec(conn))
		if debug {
		    log.Println("Sent response, probably")
		}
	}

}
