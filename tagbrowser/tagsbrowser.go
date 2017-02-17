// tagsbrowser.go
package tagbrowser

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	_ "net/http/pprof"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/tchap/go-patricia/patricia"
)

var defaultManor *Manor

var BoundariesRegex = regexp.MustCompile("\\b|\\p{Z}+|\\p{C}|\\s+|\\/+|\\.+|\\\\+|_+")

var FragsRegex = regexp.MustCompile(`(\s+|,+|;+|:+|"+|'+|\.|/+|\+|-+|_+|=+|}+|{+)`) //regexp.MustCompile("(\\/+|\\.+|\\\\+|\\-+|\\_+|_+|\\\\(|\\\\|\\p{Z}+|\\p{C}|\\s+|:|,|\"|{|}|))")

var FragsString = "\\b|\\p{Z}+|\\p{C}|\\s+|\\/+|\\.+|\\\\+|_+|\\\\(|\\\\)"

var rpcClient *rpc.Client

var debug = false
var profile = true

var wg = sync.WaitGroup{}

var maxTagLength = 50

var linux = false

func predictFiles(aStr string) []string {
	//var searchRegex = regexp.MustCompile(fmt.Sprintf("^%v", aStr))
	files, _ := ioutil.ReadDir("./")
	var searchResults = []string{}
	for _, k := range files {
		val, _ := regexp.MatchString(fmt.Sprintf("^%v", aStr), k.Name())
		if val {
			searchResults = append(searchResults, k.Name())
		}
	}
	return searchResults
}

func (s *tagSilo) resultsToTransmittable(input resultRecordCollection) []ResultRecordTransmittable {
	output := []ResultRecordTransmittable{}
	for _, v := range input {
		printStrings := []string{}
		for _, f := range v.fingerprint {
			printStrings = append(printStrings, s.getString(f))
		}
		output = append(output, ResultRecordTransmittable{v.filename, fmt.Sprintf("%v", v.line), printStrings, v.sample, fmt.Sprintf("%v", v.score)})
	}
	return output
}

func (r ResultRecordTransmittableCollection) Less(i, j int) bool {
	a := r[i].Score
	b := r[j].Score
	if a > b {
		return true
	} else {
		return false
	}
}

func (r ResultRecordTransmittableCollection) Len() int {
	return len(r)
}

func (a ResultRecordTransmittableCollection) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (r resultRecordCollection) Less(i, j int) bool {
	a := r[i].score
	b := r[j].score
	if a > b {
		return true
	} else {
		return false
	}
}

func (r resultRecordCollection) Len() int {
	return len(r)
}

func (a resultRecordCollection) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func uniqStrings(strings []string) []string {
	aHash := map[string]bool{}
	for _, v := range strings {
		aHash[v] = true
	}
	out := []string{}
	for v, _ := range aHash {
		out = append(out, v)
	}
	return out
}

func ReSplit(seps []string, in []string) []string {

	for _, sep := range seps {
		out := []string{}
		//r := regexp.MustCompile(sep)
		//log.Println("Splitting on ", sep)
		for _, elem := range in {
			//fragments := r.Split(elem, 999)
			fragments := strings.Split(elem, sep)
			for _, f := range fragments {
				if len(f) > 1 {
					out = append(out, f)
				}
			}
		}
		in = out
	}
	//log.Printf("Fragments: %V\n", uniqStrings(in))
	return uniqStrings(in)
}

func calcRawScore(aStr string) (string, int) {
	if aStr[len(aStr)-1:len(aStr)] == "-" {
		return aStr[0 : len(aStr)-1], -1
	} else {
		return aStr, 1
	}
}

func RegSplit(text string, reg *regexp.Regexp) []string {
/*
	indexes := reg.FindAllStringIndex(text, -1)
	laststart := 0
	result := make([]string, len(indexes)+1)
	for i, element := range indexes {
		result[i] = text[laststart:element[0]]
		laststart = element[1]
	}
	result[len(indexes)] = text[laststart:len(text)]
	return result
*/

    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")
    text = reg.ReplaceAllString(text, " ")

    result := strings.Split(text, " ")
    log.Println("Split results:", result)
    return result

}
func match_trie(string_table *patricia.Trie, key patricia.Prefix) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error while matching string", r, ", retrying")
			time.Sleep(1 * time.Second)
			match_trie(string_table, key)
		}
	}()

	val := string_table.Match(key)

	return val
}

func read_trie(string_table *patricia.Trie, key patricia.Prefix) int {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error while reading string", r, ", retrying")
			time.Sleep(1 * time.Second)
			read_trie(string_table, key)
		}
	}()

	val := string_table.Get(key)
	if val == nil {
		log.Printf("Got nil for string table lookup (key:%v)", key)
		return 0
	}
	return val.(int)
}

/*func scanDatabase(aFing searchPrint) resultRecordCollection {
	results := resultRecordCollection{}
	for _, elem := range database {
		if score(aFing, elem) > 0 {
			//FIXME limit result set to 1000, replace the lowest scoring result with the better new result
			results = append(results, resultRecord{
				elem.filename, elem.line, elem.fingerprint, "", score(aFing, elem)})
			sort.Sort(results)
			if len(results) > 100 {
				results = results[0:99]
			}
		}
	}
	for index, r := range results {
		results[index].sample = getLine(r.filename, r.line)
	}
	return results
}*/

func contains(aList []*record, b *record) bool {
	for _, v := range aList {
		if reflect.DeepEqual(v, b) {
			return true
		}
	}
	return false
}

func testRPC(serverAddress string) {
	time.Sleep(5 * time.Second)
	if debug {
		log.Println("Starting RPC tests")
	}
	client, err := jsonrpc.Dial("tcp", serverAddress)

	if err != nil {
		log.Println("dialing:", err)
		shutdown()
	}
	args := &Args{"the", 10}
	preply := &Reply{}
	err = client.Call("TagResponder.SearchString", args, preply)
	if err != nil {
		log.Println("RPC error:", err)
		shutdown()
	}
	if debug {
		fmt.Printf("%v", *preply)
		for _, v := range preply.C {
			fmt.Printf("Score: %v, file: %v (%v)\n", v.Score, v.Filename, v.Fingerprint)
		}
	}

	sreply := &StatusReply{}
	err = client.Call("TagResponder.Status", args, sreply)
	if err != nil {
		log.Println("RPC error:", err)
	}
	if debug {
		fmt.Println("Status: ", *sreply)
		log.Println("RPC tests complete")
	}
}

func (s *tagSilo) dumpFingerprint(f fingerPrint) {
	for _, v := range f {
		log.Printf("%v:%v", v, s.getString(v))
	}
}
func shutdown() {
	//Shut down resources so the display thread doesn't panic when the display driver goes away first
	//When we get a file persistence layer, it will go here
	os.Exit(0)

}
func stopServer() {
	//Delete silos here?
}

var cpuprofile *string

func StartServer() {
	var preAllocSize = 1000000
	var config_location = "tagdb.conf"
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	flag.IntVar(&preAllocSize, "preAlloc", preAllocSize, fmt.Sprintf("Allocate this many entries at startup.  Default: %v", preAllocSize))
	flag.BoolVar(&debug, "debug", debug, fmt.Sprintf("Print extra debugging information.  Default: %v", debug))
	flag.StringVar(&config_location, "config", config_location, "Config file to load settings from")

	flag.Parse()

	var config tomlConfig
	if _, err := toml.DecodeFile(config_location, &config); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	//Blank entry at 0

log.Println("Starting rpc server on ", ServerAddress)
	go rpc_server(ServerAddress)
	rpcClient, _ = jsonrpc.Dial("tcp", ServerAddress)

	//time.Sleep(5 * time.Second)
	log.Println("Loaded config: ", config)
	defaultManor = createManor(config)
	//silo = createSilo(false, preAllocSize, "1", 1000000)

	//test()

	//go testRPC(ServerAddress)
}
