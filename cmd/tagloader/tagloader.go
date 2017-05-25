// loader.go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/donomii/tagdb/tagbrowser"
	"github.com/ungerik/go-dry"
)

var noContents = false
var rpcClient *rpc.Client
var profile = false
var debug = false
var verbose = false
var slashes_regexp = regexp.MustCompile("\\\\")
var wg sync.WaitGroup

func wantContent(aPath string, fileSize int64) bool {
	if noContents {
		return false
	}
	if fileSize > 3000000 {
		return false
	}
	match, _ := regexp.MatchString("exe$|a$|elf$|macho$|elf4$", aPath)
	if match {
		return false
	} else {
		return true
	}
}

func DirWalk(directory string, aFunc func(string, string, string)) {
	dirs, _ := dry.ListDirDirectories(directory)
	for _, dir := range dirs {
		if debug {
			log.Println("Recursing into ", path.Join(directory, dir))
		}
		dirPath := path.Join(directory, dir)
		//wg.Add(1)
		DirWalk(dirPath, aFunc)
	}
	files, _ := dry.ListDirFiles(directory)
	for _, file := range files {
		if debug {
			log.Println("Calling user function on path ", path.Join(directory, file))
		}
		filePath := path.Join(directory, file)
		aFunc(filePath, directory, file)
	}

	//wg.Done()
}

func countSpaces(aStr string) int {
	c := 0
	for _, e := range aStr {
		if e == 32 {
			c++
		}
	}
	return c
}

func getLine(aPath string, line int) string {
	content, err := dry.FileGetString(aPath)
	if err == nil {
		lines := regexp.MustCompile("\\n|\\r\\n").Split(content, 99999)
		//log.Printf("line %v out past end of file in %v", line, aPath)
		if line < 0 {
			return "" //This is a filename record, not a line-inside-a-file record
		} else {
			return lines[line]
		}

	} else {
		return fmt.Sprintf("Could not retrieve line: %v", err)
	}
}

func insertRec(aPath string, number int, f []string) {
	wg.Add(1)
	defer wg.Done()
	args := makeArgs(aPath, number, f)
	reply := &tagbrowser.SuccessReply{}
	if rpcClient != nil {
		rpcClient.Call("TagResponder.InsertRecord", args, reply)
	}
	if reply.Success == false {
		log.Printf("Insert record failed: %v", reply.Reason)
		time.Sleep(1.0 * time.Second)
		var serr error
		rpcClient, serr = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		if serr != nil {
			log.Println("Could not connect to server: ", serr)
		}
		insertRec(aPath, number, f)
	}

}

func processFile(aPath string, fileNameFingerprint []string) {

	fileLength := dry.FileSize(aPath)
	if wantContent(aPath, fileLength) {

		content, err := dry.FileGetString(aPath)

		if err == nil {
			count := countSpaces(content)
			if float64(fileLength)/float64(count) > 15 {
				//This probably isn't a text file
				return
			}
			lines := regexp.MustCompile("\\n|\\r\\n").Split(content, 99999)
			//var totalLines = 0
			for number, l := range lines {

				f := tagbrowser.ReSplit([]string{"\n", " ", "/", ".", ",", "+", "_", "(", ")", "{", "}", "\"", "&", ";", ":", "-", "#", "!", "^", "'", "$", "=", "*", "[", "]", ">", "<", " ", "	","，", "。", "|", "」", "、", "「"}, []string{strings.ToLower(l)})
				f = append(f, fileNameFingerprint...)
				insertRec(aPath, number+1, f)
				//	totalLines = number
			}
			if verbose {
				log.Printf("Loaded %v lines from %v", len(lines), aPath)
			}

		}
	}
}
func makeArgs(aPath string, number int, f []string) *tagbrowser.InsertArgs {
	url := slashes_regexp.ReplaceAllLiteralString(aPath, "/")
	args := &tagbrowser.InsertArgs{fmt.Sprintf("%s", url), number, f}
	return args
}

func actuallyProcessFile(fullPath string) {
    p := fullPath
    var i int
    for i = 0; i < 30; i++ {
        p = tagbrowser.FragsRegex.ReplaceAllString(p, " ")

    }
    nf := strings.Fields(strings.ToLower(p))
    if debug {
        log.Printf("Inserting %v", strings.Join(nf, ","))
    }

    insertRec(fullPath, -1, nf)

    processFile(fullPath, nf)
}


func processPaths(aCh chan []string) {
	if debug {
		log.Println("Worker starting: processPaths")
	}
	for elem := range aCh {

		fullPath := elem[0]
		if verbose {
			log.Println("Processing path ", fullPath)
		}
        actuallyProcessFile(fullPath)
		wg.Done()
	}
}

func scanDir(aPath string, aCh chan []string) {
	defer wg.Done()
	//fmt.Println("Scanning directories")
	if profile {
		prof_file, _ := os.Create("tagsbrowser.cpuprofile")
		pprof.StartCPUProfile(prof_file)
		defer pprof.StopCPUProfile()
	}
	//wg.Add(1)

	DirWalk(aPath, func(aPath, aDir, aFile string) {
		wg.Add(1)
		aCh <- []string{aPath, aDir, aFile}
	})

}

var numworkers = 1

func main() {

	flag.BoolVar(&noContents, "noContents", false, "Do not look inside files")
	loadFromArgs := false
	wantHelp := false
	flag.BoolVar(&loadFromArgs, "addRecord", false, "Add record from the command line")
	flag.BoolVar(&wantHelp, "help", false, "Display help")
	flag.BoolVar(&verbose, "verbose", false, "Show files as they are loaded")
	flag.BoolVar(&debug, "debug", false, "Display additional debug information")
	flag.IntVar(&numworkers, "parallel", 1, "Maximum number of simultaneous inserts to attempt")
	flag.StringVar(&tagbrowser.ServerAddress, "server", tagbrowser.ServerAddress, fmt.Sprintf("Server IP and Port.  Default: %s", tagbrowser.ServerAddress))
	flag.Parse()
	dirs := flag.Args()
	if len(dirs) < 1 || wantHelp {
		fmt.Println("Use: loader.exe  <--noContents>  directory")
		fmt.Println("Use: loader.exe  --addRecord  <path> <offset> tag1 tag2 tag3 tag4")
		fmt.Println("")
		fmt.Println("Recursively scan a directory or add a tag directly to the database")
		fmt.Println("")
		fmt.Println("Scanning a directory will add one record for each file, plus a record for each line inside that file.  This allows tagbrowser to do powerful searches to find the exact line in a file.")
		fmt.Println("")
		fmt.Println("	--verbose		Print extra information")
		fmt.Println("	--debug			Print extra debug information")
		fmt.Println("	--noContents	Add filenames to the database, but do not look at the contents of the files")
		fmt.Println("	--addRecord		Add a record directly from the command line.  The format is:")
		fmt.Println("		path	The location (usually a URL)")
		fmt.Println("		offset	An arbitrary number.  Can be index into the data")
		fmt.Println("		tag1...	Tags for this record.  This is what tagbrowser uses when searching")
		//fmt.Println("Using current directory")
		//dirs = []string{"."}
		os.Exit(1)
	}
	if debug {
		log.Println("Printing extra debugging information")
        if verbose {
            log.Println("Printing files as they are loaded")
        }
	}
	if noContents {
		log.Println("Ignoring file contents, only loading file names")
	}
	var err error
	if loadFromArgs {
		index, _ := strconv.ParseInt(dirs[1], 0, 0)
		args := &tagbrowser.InsertArgs{dirs[0], int(index), dirs[2:]}
		reply := &tagbrowser.SuccessReply{false, ""}
		if debug {
			log.Println("Connecting to server on ", tagbrowser.ServerAddress)
		}
		rpcClient, err = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		if err != nil {
			log.Println("Could not connect to server: ", err)
			os.Exit(1)
		}
		rpcClient.Call("TagResponder.InsertRecord", args, reply)
	} else {
		if debug {
			log.Println("Connecting to server on ", tagbrowser.ServerAddress)
		}
		rpcClient, err = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		if err != nil {
			log.Println("Could not connect to server: ", err)
			os.Exit(1)
		}
		if debug {
			log.Println("Server connected established, loading...")
		}
		pathsCh := make(chan []string)

		for i := 0; i < numworkers; i = i + 1 {
			go processPaths(pathsCh)
		}
		for _, searchDir := range dirs {
			if debug {
				fmt.Printf("Scanning %v", searchDir)
			}
            if info, err := os.Stat(searchDir); err == nil && info.IsDir() {
                wg.Add(1)
                go scanDir(searchDir, pathsCh)
            } else {
                actuallyProcessFile(searchDir)
            }
		}
		wg.Wait()
		//for true {
		//	time.Sleep(1 * time.Second)
		//}
	}

}
