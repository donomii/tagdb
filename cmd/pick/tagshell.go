// status.go
package main

import (
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/donomii/tagdb/tagbrowser"

	//"strings"
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	//"sort"
	"net/rpc"

	"github.com/nsf/termbox-go"

	"sync"
)

var serverActive = false

var use_gui = true

var statuses map[string]string
var results []tagbrowser.ResultRecordTransmittable

var selection = 0
var itempos = 0
var cursorX = 11
var cursorY = 1
var selectPosX = 11
var selectPosY = 1
var focus = "input"
var inputPos = 0
var searchStr string
var debugStr = ""
var client *rpc.Client

var predictResults []string
var lines []string
var linesTr []tagbrowser.ResultRecordTransmittable

var refreshMutex sync.Mutex

var LineCache map[string]string

func FetchLine(f string, lineNum int) (line string, lastLine int, err error) {
	key := fmt.Sprintf("%v%v", f, lineNum)
	if val, ok := LineCache[key]; ok {
		return val, -1, nil
	} else {
		r, _ := os.Open(f)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lastLine++
			if lastLine == lineNum {
				LineCache[key] = sc.Text()
				return sc.Text(), lastLine, sc.Err()
			}
		}
		LineCache[key] = line
		return line, lastLine, io.EOF
	}
}

func score(a searchPrint, b tagbrowser.ResultRecordTransmittable) int {
	score := 0
	//We can do much better than a nested loop here

	for _, vv := range b.Fingerprint {
		for _, v := range a.wanted {
			if v == vv {
				score += 1

			}

		}
		for _, v := range a.unwanted {
			if v == vv {
				score -= 1

			}

		}
	}
	//fmt.Println("----")
	return score
}

type fingerPrint []string

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
	//log.Println("Split results:", result)
	return result

}

var FragsRegex = regexp.MustCompile(`(\s+|,+|;+|:+|"+|'+|\.|/+|\+|-+|_+|=+|}+|{+|>+|<+|!+|\)+|\(+)`) //regexp.MustCompile("(\\/+|\\.+|\\\\+|\\-+|\\_+|_+|\\\\(|\\\\|\\p{Z}+|\\p{C}|\\s+|:|,|\"|{|}|>|<|!|))")

func makeFingerprintFromData(aStr string) fingerPrint {
	//seps := []string{"\\\\", "\\.", " ", "\\(", "\\)", "/", "_", "\\b"} //\\b|\\p{Z}+|\\p{C}|\\s+|\\/+|\\.+|\\\\+|_+
	//return makeFingerprint(ReSplit(seps, strings.Fields(strings.ToLower(aStr))))

	return makeFingerprint(RegSplit(strings.ToLower(aStr), FragsRegex))
}

func makeFingerprint(fragments []string) fingerPrint {
	sort.Strings(fragments)
	frags := map[string]int{}

	index := 0
	for _, f := range fragments {

		if len(f) > 1 && len(f) < maxTagLength {

			key, _ := calcRawScore(f)
			_, ok := frags[key]
			if !ok {
				frags[key] = index
				index = index + 1
			}
		} else {
			if len(f) > maxTagLength {
				//				s.LogChan["warning"] <- fmt.Sprintln("Rejecting tag as too long: ", f)
			}
		}

	}
	//log.Printf("Fingerprint of length: %v", index)
	fingerprint := make(fingerPrint, index)
	for k, i := range frags {
		//fingerprint = append(fingerprint, k)
		//log.Printf("Assigning to index %v", i)
		fingerprint[i] = k
	}

	return fingerprint
}

type searchPrint struct {
	wanted   fingerPrint
	unwanted fingerPrint
}

func calcRawScore(aStr string) (string, int) {
	if aStr[len(aStr)-1:len(aStr)] == "-" {
		return aStr[0 : len(aStr)-1], -1
	} else {
		return aStr, 1
	}
}

var maxTagLength = 100

func makeSearchPrint(fragments []string) searchPrint {

	//Pull this out into a separate function FIXME
	frags := map[string]int{}
	for _, f := range fragments {
		if len(f) > 1 && len(f) < maxTagLength {

			key, rawScore := calcRawScore(f)

			frags[key] = rawScore

		} else {
			log.Println("Rejected tag as too short or too long:", f)
		}
	}
	searchP := searchPrint{}
	for k, v := range frags {
		//fmt.Printf("k: %v, v: %v\n", k, v)
		if v > 0 {
			searchP.wanted = append(searchP.wanted, k)
			//fmt.Printf("Storing k: %v in wanted\n", k)
		} else {
			//fmt.Printf("Storing k: %v in unwanted\n", k)
			searchP.unwanted = append(searchP.unwanted, k)
		}
	}
	return searchP
}

//Contact server with search string
func search(searchTerm string, numResults int) []tagbrowser.ResultRecordTransmittable {
	statuses["Status"] = "Searching"
	if searchTerm == "" {
		return linesTr
	}

	pr := makeSearchPrint(RegSplit(strings.ToLower(searchTerm), FragsRegex))

	var out []tagbrowser.ResultRecordTransmittable
	for _, v := range linesTr {
		s := score(pr, v)
		v.Score = fmt.Sprintf("%v", s)
		if s > 0 {
			out = append(out, v)
		}
		if len(out) > numResults {
			break
		}
	}

	statuses["Status"] = "Search complete"
	return out
}

var completeMatch = false

func isLinux() bool {
	return (runtime.GOOS == "linux")
}

func isDarwin() bool {
	return (runtime.GOOS == "darwin")
}

func refreshTerm() {

	//statuses["Screen"] = "Refresh"
	if use_gui {
		refreshMutex.Lock()
		defer refreshMutex.Unlock()
		termbox.Clear(foreGround(), backGround())
		putStr(0, 0, debugStr)
		putStr(0, 1, fmt.Sprintf("Search for:%v", searchStr))
		_, height := termbox.Size()
		//prevRecord := tagbrowser.ResultRecordTransmittable{"", -1, makeFingerprintFromData(""), "", 0}
		prevRecord := tagbrowser.ResultRecordTransmittable{}
		dispLine := 2
		if len(results) > 0 {
			//putStr(5, dispLine, fmt.Sprintf("%v (line %v)", results[0].Filename, results[0].Line))
			putStr(5, dispLine, results[0].Sample)
			dispLine++
			prevRecord = results[0]
		}
		itempos = 0
		for i, elem := range results {
			if dispLine < height-4 {
				if !(i == 0) && !(elem.Filename == prevRecord.Filename) {
					//putStr(3, dispLine, fmt.Sprintf("%v", elem.Filename))
					putStr(3, dispLine, fmt.Sprintf("%v", elem.Sample))
					dispLine++
				}
				if itempos == selection {
					putStr(0, dispLine, "*")
					selectPosX = 0
					selectPosY = dispLine
				}
				//if elem.Line != "-1" && strings.HasPrefix(elem.Filename, "http") {
				putStr(1, dispLine, fmt.Sprintf("%v", elem.Score))
				//l, _ := strconv.Atoi(elem.Line)
				//LineStr, _, _ := FetchLine(elem.Filename, l)
				//putStr(8, dispLine, fmt.Sprintf("(line %v) %v", elem.Line, LineStr))
				putStr(8, dispLine, fmt.Sprintf("(line %v) %v", elem.Line, elem.Sample))
				dispLine++
				itempos++
				prevRecord = elem
				//}
			}
		}
		putStr(1, height-3, fmt.Sprintf("%v results", len(results)))
		putStr(20, height-3, fmt.Sprintf("%v", statuses))
		putStr(1, height-2, fmt.Sprintf("Type your search terms, add a - to the end of word to remove that word (word-)"))
		putStr(1, height-1, fmt.Sprintf("Up/Down Arrows to select a result, Right Arrow to edit that file, Escape Quits"))
		if focus == "input" {
			putStr(8, 9, "                    ")
			for i, v := range predictResults {
				if i < 10 {
					putStr(8, 9, "-----Suggestions----")
					putStr(8, 10+i, "|                  |")
					putStr(8, 10+i+1, "--------------------")
					putStr(10, 10+i, v)
				}
			}
		}

		if focus == "input" {
			termbox.SetCursor(11+inputPos, 1)
		} else {
			termbox.SetCursor(selectPosX, selectPosY)
		}
		termbox.Flush()
	}
}

//Find the first space character to the left of the cursor
func searchLeft(aStr string, pos int) int {
	for i := pos; i > 0; i-- {
		if aStr[i-1] == ' ' {
			if pos != i {
				return i
			}
		}
	}
	return 0
}

//Find the first space character to the right of the cursor
func searchRight(aStr string, pos int) int {
	for i := pos; i < len(aStr)-1; i++ {
		if aStr[i+1] == ' ' {
			if pos != i {
				return i
			}
		}
	}
	return len(aStr) - 1
}

func extractWord(aLine string, pos int) string {
	start := searchLeft(aLine, pos)
	return aLine[start:pos]
}
func doInput() {

	if use_gui {
		//statuses["Input"] = "Waiting"
		//width, height := termbox.Size()
		ev := termbox.PollEvent()
		if ev.Type == termbox.EventKey {
			if ev.Mod == termbox.ModAlt {
				switch ev.Key {
				case termbox.KeyArrowRight:
					inputPos = searchRight(searchStr, inputPos)
				case termbox.KeyArrowLeft:
					inputPos = searchLeft(searchStr, inputPos)
				}
			} else {
				//statuses["Input"] = fmt.Sprintf("%v", ev.Key) //"Processing"
				//debugStr = fmt.Sprintf("key: %v, %v, %v", ev.Key, ev.Ch, ev)
				switch ev.Key {
				case termbox.KeyArrowRight:
					if isLinux() || isDarwin() {
						termbox.Close()
						fmt.Fprintf(os.Stderr, "%v\n", results[selection].Sample)
						termbox.Init()
						refreshTerm()

					} else {
						fmt.Fprintf(os.Stderr, "%v\n", results[selection].Sample)
						refreshTerm()
					}
					shutdown()
				case termbox.KeyArrowDown:
					selection++
					if selection > len(results) {
						selection = len(results)
					}
					focus = "selection"
				case termbox.KeyArrowUp:
					selection--
					if selection < 0 {
						selection = 0
					}
					focus = "selection"
				case termbox.KeyEsc:
					shutdown()
				case termbox.KeyBackspace, termbox.KeyBackspace2:
					if len(searchStr) > 0 {
						searchStr = searchStr[0 : len(searchStr)-1]
						inputPos -= 1
					}

					focus = "input"
					refreshTerm()
				case termbox.KeyEnter:

					results = search(searchStr, 50)

					//sort.Sort(results) FIXME
					focus = "selection"
					refreshTerm()
				case termbox.KeySpace:
					searchStr = fmt.Sprintf("%s ", searchStr)
					inputPos += 1
					refreshTerm()

				default:
					//statuses["Input"] = ev.Key
					searchStr = fmt.Sprintf("%s%c", searchStr, ev.Ch)
					results = search(searchStr, 50)
					//sort.Sort(results) FIXME

					inputPos += 1
					cursorX = 11 + len(searchStr)
					cursorY = 1
					focus = "input"
					refreshTerm()
				}
			}
		}
	}
}

//ForeGround colour
func foreGround() termbox.Attribute {
	return termbox.ColorBlack
}

//Background colour
func backGround() termbox.Attribute {
	return termbox.ColorWhite
}

//Display a string at XY
func putStr(x, y int, aStr string) {
	width, height := termbox.Size()
	if y >= height {
		return
	}
	for i, r := range aStr {
		if x+i >= width {
			return
		}
		termbox.SetCell(x+i, y, r, foreGround(), backGround())
	}
}

//Redraw screen every 200 Milliseconds
func automaticRefreshTerm() {
	for i := 0; i < 1; i = 0 {
		refreshTerm()
		time.Sleep(time.Millisecond * 200)
		if !serverActive {
			statuses["Status"] = "Closed"
			return
		}
	}
}

func automaticdoInput() {
	for i := 0; i < 1; i = 0 {
		doInput()
		time.Sleep(20 * time.Millisecond)
		if !serverActive {
			statuses["Input"] = "Closed"
			return
		}
	}
}

//Clean up and exit
func shutdown() {
	//Shut down resources so the display thread doesn't panic when the display driver goes away first
	//When we get a file persistence layer, it will go here
	statuses["Status"] = "Shutting down"
	use_gui = false
	serverActive = false
	os.Exit(0)

}

type LineIterator struct {
	reader *bufio.Reader
}

func NewLineIterator(rd io.Reader) *LineIterator {
	return &LineIterator{
		reader: bufio.NewReader(rd),
	}
}

func (ln *LineIterator) Next() ([]byte, error) {

	var bytes []byte
	for {
		line, isPrefix, err := ln.reader.ReadLine()
		if err != nil {
			return nil, err
		}
		bytes = append(bytes, line...)
		if !isPrefix {
			break
		}
	}
	return bytes, nil
}

func slurpSTDIN() []string {
	arr := make([]string, 0)
	ln := NewLineIterator(os.Stdin)
	for {
		line, err := ln.Next()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				log.Fatal(err)
			}
		}
		arr = append(arr, string(line))
	}

	statuses["Input"] = fmt.Sprintf("%v lines", len(arr))
	return arr
}
func main() {
	LineCache = map[string]string{}
	flag.StringVar(&tagbrowser.ServerAddress, "server", tagbrowser.ServerAddress, fmt.Sprintf("Server IP and Port.  Default: %s", tagbrowser.ServerAddress))
	flag.Parse()
	//terms := flag.Args()
	//if len(terms) < 1 {
	//	fmt.Println("Use: query.exe  < --completeMatch >  search terms")
	//}
	searchStr = ""

	refreshMutex = sync.Mutex{}
	predictResults = []string{}
	results = []tagbrowser.ResultRecordTransmittable{}
	statuses = map[string]string{}

	termbox.Init()
	termbox.SetInputMode(termbox.InputEsc)
	//termbox.SetInputMode(termbox.InputAlt)
	defer termbox.Close()
	use_gui = true
	serverActive = true
	go automaticRefreshTerm()

	go automaticdoInput()

	statuses["Server"] = "Reading Input"
	lines := slurpSTDIN()
	linesTr = []tagbrowser.ResultRecordTransmittable{}
	for _, l := range lines {
		r := tagbrowser.ResultRecordTransmittable{"", "", makeFingerprintFromData(l), l, "0"}
		linesTr = append(linesTr, r)
	}
	statuses["Lines"] = fmt.Sprintf("%v", len(linesTr))
	statuses["Server"] = "Read Complete"
	results = search(searchStr, 50)
	refreshTerm()
	for {
		time.Sleep(1 * time.Second)
	}

}
