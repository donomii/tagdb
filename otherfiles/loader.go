// loader.go
package main

import (
	"donomii/tagbrowser"
	"dry"
	"flag"
	"fmt"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

var noContents = false
var rpcClient *rpc.Client
var profile = false
var debug = false

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
		//fmt.Println("Recursing into ", dir)
		dirPath := path.Join(directory, dir)
		//wg.Add(1)
		DirWalk(dirPath, aFunc)
	}
	files, _ := dry.ListDirFiles(directory)
	for _, file := range files {
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

				f := tagbrowser.RegSplit(strings.ToLower(l), tagbrowser.FragsRegex)
				f = append(f, fileNameFingerprint...)
				args := &tagbrowser.InsertArgs{aPath, number, f}
				reply := &tagbrowser.SuccessReply{}
				rpcClient.Call("TagResponder.InsertRecord", args, reply)
				//	totalLines = number
			}
			//log.Printf("Loaded %v lines from %v", totalLines+1, aPath)

		}
	}
}

func processPaths(aCh chan []string) {
	for elem := range aCh {

		fullPath := elem[0]
		//log.Println(fullPath)

		nf := tagbrowser.RegSplit(strings.ToLower(fullPath), tagbrowser.FragsRegex)
		//log.Printf("Inserting %v: %v", fullPath, nf)
		args := &tagbrowser.InsertArgs{fullPath, -1, nf}
		reply := &tagbrowser.SuccessReply{false, ""}
		rpcClient.Call("TagResponder.InsertRecord", args, reply)
		processFile(fullPath, nf)
		//pathFrags := regexp.MustCompile("/|\\\\").Split(elem, 99999) //fixme pass this from dirwalk
		//fname := pathFrags[len(pathFrags)-1]

		//return
		//recordCh <- record{get_or_create_symbol(fullPath), -1, nf}
		//contentRecords, _ := processFile(fullPath)

		////Combine the filename's fingerprint with the line's fingerprint, so the user can remove all lines belonging to a file
		////with the keyword- command
		//for i, _ := range contentRecords {
		//	//fmt.Println("before --")
		//	//dumpFingerprint(line_elem.fingerprint)
		//	//fmt.Println("adding --")
		//	//dumpFingerprint(nf)
		//	for _, value := range nf {
		//		contentRecords[i].fingerprint = append(contentRecords[i].fingerprint, value)
		//	}
		//	//fmt.Println("after")
		//	//dumpFingerprint(contentRecords[i].fingerprint)
		//	tags := []string{}
		//	for _, value := range contentRecords[i].fingerprint {
		//		tags = append(tags, getString(value))
		//	}
		//	args := &InsertArgs{getString(contentRecords[i].filename), contentRecords[i].line, tags}
		//	reply := &SuccessReply{}
		//	InsertRecord(args, reply)
		//	//recordCh <- contentRecords[i]
		//}

	}
}

func scanDir(aPath string, aCh chan []string) {
	//defer wg.Done()
	//fmt.Println("Scanning directories")
	if profile {
		prof_file, _ := os.Create("tagsbrowser.cpuprofile")
		pprof.StartCPUProfile(prof_file)
		defer pprof.StopCPUProfile()
	}
	//wg.Add(1)

	DirWalk(aPath, func(aPath, aDir, aFile string) {
		aCh <- []string{aPath, aDir, aFile}
	})

}

func main() {

	flag.BoolVar(&noContents, "noContents", false, "Do not look inside files")
	loadFromArgs := false
	flag.BoolVar(&loadFromArgs, "saveArgs", false, "Store the command line args as a record in the database")
	flag.Parse()
	dirs := flag.Args()
	if len(dirs) < 1 {
		fmt.Println("Use: loader.exe  <--noContents>  directory")
		fmt.Println("Use: loader.exe  --saveArgs  <path> <offset> tag1 tag2 tag3 tag4")
		fmt.Println("")
		fmt.Println("Recursively scan a directory or add a tag directly to the database")
		fmt.Println("")
		fmt.Println("Scanning a directory will add one record for each file, plus a record for each line inside that file.  This allows tagbrowser to do powerful searches to find the exact line in a file.")
		fmt.Println("")
		fmt.Println("	--noContents	Add filenames to the database, but do not look at the contents of the files")
		fmt.Println("	--saveArgs		Add a record directly from the command line.  The format is:")
		fmt.Println("						path	The location (usually a URL)")
		fmt.Println("						offset	Can be index into the url, like a like number, byte offset or character count")
		fmt.Println("						tag1...	Lookup tags for this record.  This is what tagbrowser uses when searching")
		//fmt.Println("Using current directory")
		//dirs = []string{"."}
		os.Exit(1)
	}

	if loadFromArgs {
		index, _ := strconv.ParseInt(dirs[1], 0, 0)
		args := &tagbrowser.InsertArgs{dirs[0], int(index), dirs[2:]}
		reply := &tagbrowser.SuccessReply{false, ""}
		rpcClient, _ = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		rpcClient.Call("TagResponder.InsertRecord", args, reply)
	} else {
		rpcClient, _ = jsonrpc.Dial("tcp", tagbrowser.ServerAddress)
		pathsCh := make(chan []string)

		for i := 1; i < 100; i = i + 1 {
			go processPaths(pathsCh)
		}
		for _, searchDir := range dirs {
			if debug {
				fmt.Printf("Scanning %v", searchDir)
			}

			//wg.Add(1)
			go scanDir(searchDir, pathsCh)
		}

		for true {
			time.Sleep(1 * time.Second)
		}
	}

}
