// silo
package tagbrowser

import (

	//"runtime/pprof"
	//debugModule "runtime/debug"
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	//_ "github.com/mattn/go-sqlite3"

	syncmap "github.com/donomii/genericsyncmap"
	"github.com/tchap/go-patricia/patricia"
)

func IsIn(aRecord ResultRecordTransmittable, aList ResultRecordTransmittableCollection) bool {

	dupe := false
	if len(aList) > 0 {
		for _, v := range aList {
			if equalPrints(v.Fingerprint, aRecord.Fingerprint) && v.Filename == aRecord.Filename && v.Line == aRecord.Line {
				dupe = true
			}
		}
	}
	return dupe

}

func (s *tagSilo) sumariseDatabase() (map[string]int, map[string]int) {
	hist := map[string]int{}
	topTags := []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	log.Println("Starting summary analysis")
	for t, _ := range s.tag2file {
		numFiles := len(s.tag2file[t])
		hist[fmt.Sprintf("%v", numFiles)] = hist[fmt.Sprintf("%v", numFiles)] + 1
		if numFiles > len(s.tag2file[topTags[0]]) {
			topTags[0] = t
		}
		prev := 0
		for i, _ := range topTags {
			if len(s.tag2file[topTags[prev]]) > len(s.tag2file[topTags[i]]) {
				topTags[i], topTags[prev] = topTags[prev], topTags[i]
				prev = i
			}
		}

	}

	tagHist := map[string]int{}
	for _, elem := range topTags {
		tagHist[s.getString(elem)] = len(s.tag2file[elem])
	}
	log.Println("Completed summary analysis")
	return hist, tagHist
}

func (s *tagSilo) count(name string) {
	s.counterMutex.Lock()
	defer s.counterMutex.Unlock()
	val, _ := s.counters.Load(name)
	s.counters.Store(name, val+1)
}

func (s *tagSilo) heartBeat() {
	for {
		log.Println("Silo: ", s.id)
		time.Sleep(1.0 * time.Second)
	}
}

type SerialiseMe struct {
	Id                   string
	Last_database_record int

	Database          []record //The in memory database, if any
	Counters          map[string]int
	Next_string_index int
	Last_tag_record   int
	//String_table      *patricia.Trie  //Does not serialise?

	Reverse_string_table []string
	Tag2file             [][]*record
	Tag2record           [][]int

	Temporary     bool
	Offload_index int
	Offloading    bool
	MaxRecords    int
}

func createSilo(memory bool, preAllocSize int, id string, channel_buffer int, inputChan chan RecordTransmittable, dataDir string, permanentStoreCh chan RecordTransmittable, isTemporary bool, maxRecords int, checkpointMutex *sync.Mutex, logChans map[string]chan string) *tagSilo {

	silo := &tagSilo{}
	silo.LogChan = logChans
	silo.LogChan["file"] <- fmt.Sprintln("Creating silo ", id)
	silo.memory_db = memory
	silo.id = id

	silo.writeMutex = sync.Mutex{}
	silo.readMutex = sync.Mutex{}
	silo.counterMutex = sync.Mutex{}
	silo.checkpointMutex = checkpointMutex
	silo.recordCh = make(chan record, channel_buffer)
	silo.InputRecordCh = inputChan
	silo.permanentStoreCh = permanentStoreCh
	silo.temporary = isTemporary

	silo.last_database_record = 1
	silo.offload_index = 2
	silo.maxRecords = maxRecords
	silo.string_cache = syncmap.NewSyncMap[int, string]()
	//silo.symbol_cache = map[string]int{}
	silo.symbol_cache = syncmap.NewSyncMap[string, int]()
	silo.tag_cache = syncmap.NewSyncMap[int, []int]()
	silo.record_cache = syncmap.NewSyncMap[int, record]()
	silo.counters = syncmap.NewSyncMap[string, int]()
	silo.threadsWait = sync.WaitGroup{}

	//go silo.heartBeat()
	silo.filename = fmt.Sprintf("%v/tagSilo_%s.tagdb", dataDir, id)
	if silo.memory_db {
		silo.string_table = patricia.NewTrie()
		silo.reverse_string_table = make([]string, 1, preAllocSize)
		silo.tag2file = make([][]*record, 1, preAllocSize)

		silo.database = make([]record, 1, preAllocSize)
		silo.reverse_string_table[0] = "An Error occurred, you should never have seen this"
		silo.LogChan["file"] <- fmt.Sprintf("Creating memory silo")

		var aBuff bytes.Buffer
		silo.anotherbuffer = &aBuff
		checkpointname := fmt.Sprintf("%v.checkpoint", silo.filename)
		f, ferr := os.Open(checkpointname)
		if ferr != nil {
			checkpointname := fmt.Sprintf("%v.checkpoint.bak", silo.filename)
			f, ferr = os.Open(checkpointname)
		}
		if ferr == nil {
			silo.LogChan["file"] <- fmt.Sprintln(checkpointname, " exists, reading stored data")
			defer f.Close()
			silo.dec = gob.NewDecoder(f)
			var d SerialiseMe
			dec_err := silo.dec.Decode(&d)
			if dec_err == nil {
				silo.LogChan["file"] <- fmt.Sprintln("Successfully decoded checkpoint data")
				silo.last_database_record = d.Last_database_record

				silo.database = d.Database
				//silo.counters = d.Counters
				for k, v := range d.Counters {
					silo.counters.Store(k, v)
				}
				silo.next_string_index = d.Next_string_index
				silo.last_tag_record = d.Last_tag_record

				//silo.string_table = d.String_table

				silo.reverse_string_table = d.Reverse_string_table
				silo.tag2file = d.Tag2file
				silo.tag2record = d.Tag2record

				//s.temporary     = d.Temporary
				silo.offload_index = d.Offload_index
				//s.offloading    = d.Offloading
				//silo.maxRecords = d.MaxRecords

				silo.dec = nil
				silo.LogChan["file"] <- fmt.Sprintln("Recreating string table")

				for i, v := range silo.reverse_string_table {
					silo.string_table.Insert(patricia.Prefix(v), i)
				}
				silo.LogChan["file"] <- fmt.Sprintln("String table complete, ", len(silo.reverse_string_table), " entries,  ", silo.next_string_index, " strings, ", silo.last_tag_record, " tags")
			} else {
				silo.LogChan["error"] <- fmt.Sprintln("Error decoding: ", dec_err)
			}
		}
	} else {

		silo.LogChan["file"] <- fmt.Sprintf("Opening silo %v", silo.filename)

		silo.Store = NewSQLStore(silo.filename)
		//silo.Store = NewWeaviateStore(silo.filename)
		silo.Store.Init(silo)
		/*go func() {
		      for {
		          //pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		          log.Println("Silo ", silo.id, " operational status: ", silo.Operational)
		          time.Sleep(5 * time.Second )
		       }
		  }()
		*/

		silo.LogChan["file"] <- fmt.Sprintf("Opened file %v", silo.filename)

	}
	silo.Operational = true

	silo.threadsWait.Add(1)
	go silo.storeRecordWorker()

	if silo.temporary {
		silo.threadsWait.Add(1)
		go silo.offloadWorker()
	}
	if silo.memory_db {
		silo.threadsWait.Add(1)
		go silo.storeMemRecordWorker()
		silo.threadsWait.Add(1)
		go silo.checkpointWorker()

	} else {
		silo.threadsWait.Add(1)
		go silo.storePermanentRecordWorker()
		silo.threadsWait.Add(1)
		go silo.SQLCommitWorker()
	}

	silo.threadsWait.Add(1)
	go silo.monitorSiloWorker()

	silo.LogChan["file"] <- fmt.Sprintln("Silo operational, ", len(silo.reverse_string_table), " entries,  ", silo.next_string_index, " strings, ", silo.last_tag_record, " tags")
	if debug {
		log.Println("Silo operational status: ", silo.Operational)
		log.Println("Create silo finished")
	}

	return silo
}

func (tSilo *tagSilo) test() {
	testRec := record{tSilo.get_or_create_symbol("Foods"), -1, tSilo.makeFingerprintFromData("pork chicken beef")}

	key, rscore := calcRawScore("word")
	if !((key == "word") && (rscore == 1)) {
		panic(fmt.Sprintf("caclRawScore returned %v instead of 'word'", key))
	}
	key, rscore = calcRawScore("word-")
	if !((key == "word") && (rscore == -1)) {
		panic(fmt.Sprintf("caclRawScore returned %v instead of 'word'", key))
	}
	//LiteIDE is not unicode-aware?!?
	//key, score = calcRawScore("山-")
	//if !((key == "山") && (score == -1)) {
	//	panic(fmt.Sprintf("caclRawScore returned %v instead of '山'", key))
	//}
	//	testFprint := makeFingerprintFromSearch("chicken beef-")
	//	if !(testFprint[get_or_create_symbol("chicken")] == 1) {
	//		panic(fmt.Sprintf("Invalid score for %v", testFprint))
	//	}
	//	if !(testFprint[get_or_create_symbol("beef")] == -1) {
	//		panic(fmt.Sprintf("Invalid score for %v", testFprint))
	//	}
	//	testScore := score(testFprint, testRec)
	//	if !(testScore == 0) {
	//		panic("Invalid score")
	//	}
	fp1 := tSilo.makeFingerprintFromSearch("chicken beef pork")
	testScore := tSilo.score(fp1, testRec)
	if !(testScore == 3) {
		log.Printf("Invalid score: %v, comparing %v and %v", testScore, testRec, fp1)
		os.Exit(1)
	}
	fp := tSilo.makeFingerprintFromData("filename")
	testRec.Fingerprint = append(testRec.Fingerprint, fp[0])
	testScore1 := tSilo.score(tSilo.makeFingerprintFromSearch("filename"), testRec)
	if !(testScore1 == 1) {
		panic("Invalid score")
	}
	sp := tSilo.makeFingerprintFromSearch("filename-")
	if debug {
		fmt.Printf("Test: ")
		fmt.Printf("\nWanted ")
		tSilo.dumpFingerprint(sp.wanted)
		fmt.Printf("\nUnwanted ")
		tSilo.dumpFingerprint(sp.unwanted)
		fmt.Printf("\ntestrec ")
		tSilo.dumpFingerprint(testRec.Fingerprint)
	}
	testScore = tSilo.score(sp, testRec)
	if debug {
		fmt.Printf("Score: %v\n", testScore)
	}
	if !(testScore == -1) {
		panic("Invalid score")
	}
	sp = tSilo.makeFingerprintFromSearch("filename- pork")
	testScore = tSilo.score(sp, testRec)
	if !(testScore == 0) {
		panic("Invalid score")
	}
	testScore = tSilo.score(tSilo.makeFingerprintFromSearch("pork chicken beef-"), testRec)
	if !(testScore == 1) {
		panic(fmt.Sprintf("Invalid score for %v:%v", "pork chicken beef-", testScore))
	}
	//	testScore = score(makeFingerprintFromSearch("pork chicken beef-"), testRec)
	//	if !(testScore == 1) {
	//		panic(fmt.Sprintf("Invalid score for %v:%v", testFprint, testScore))
	//	}
	//panic(fmt.Sprintf("%v", makeFingerprint("trie trie")))
}
