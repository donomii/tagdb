// farm.go

//A farm manages several silos, which are files on the disk.  A farm is a directory of silos.  The farm object manages these silos
//It sends records to the silos to be stored, and queries the silos and combines the search results

package tagbrowser

import (
    "os"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

type Farm struct {
	silos            []*tagSilo
	recordCh         chan RecordTransmittable //Used to send records to the silos
	permanentStoreCh chan RecordTransmittable
	temporary        bool
	location         string
	memory_only      bool
	maxSilos         int
	maxRecords       int
	checkpointMutex  sync.Mutex
	ShutdownStatus   bool
	LockLog          chan string
	LogChan          map[string]chan string
}

func (f *Farm) Shutdown() {
	f.ShutdownStatus = true
	for _, s := range f.silos {
		s.ReadOnly = true

	}
	time.Sleep(time.Second * 5.0)
	for _, s := range f.silos {
		s.Operational = false

	}
	time.Sleep(time.Second * 10.0)
	for _, s := range f.silos {
		s.Checkpoint()
		log.Printf("Silo %v", s.id)

	}
}

func monitorSilosWorker(f *Farm) {
	var total_silos int
	for i := 0; i < 1; i = 0 {
		if f.ShutdownStatus {
			return
		}
		time.Sleep(time.Millisecond * 500.0)
		total_silos = 0
		if f.temporary {
			//log.Println("Checking memory silos: ", len(f.silos), " of ", f.maxSilos, " possible")
			for _, s := range f.silos {
				if s != nil && s.Operational {
					//log.Printf("Silo %v, %v records, %v offloaded", s.id, s.last_database_record, s.offload_index)
					total_silos = total_silos + 1
				} else {
					//f.silos[i] = nil
				}
			}
			if total_silos < f.maxSilos {
				aSilo := createSilo(f.memory_only, f.maxSilos, fmt.Sprintf("%v", len(f.silos)), 0, f.recordCh, f.location, f.permanentStoreCh, f.temporary, f.maxRecords, f.checkpointMutex, f.LogChan)
				aSilo.LockLog = f.LockLog
				aSilo.LogChan = f.LogChan
				if f.temporary {
					aSilo.offloading = true
				}
				f.silos = append(f.silos, aSilo)

				log.Println("Added new silo:", aSilo.id)
			}
		}
	}
}

func printLogWorker(ch chan string) {
	time.Sleep(time.Second * 2.0)
	for line := range ch {
		log.Println(line)
	}
}

func ignoreLogWorker(ch chan string) {
	time.Sleep(time.Second * 2.0)
	for _ = range ch {

	}
}

func createFarm(location string, number_of_silos int, inputchan chan RecordTransmittable, memory_only bool, permanentStoreCh chan RecordTransmittable, isTemporary bool, maxRecords int) *Farm {
	f := Farm{}

	f.LockLog = make(chan string, 100)
	f.LogChan = map[string]chan string{}
	f.LogChan["file"] = make(chan string, 100)
	f.LogChan["database"] = make(chan string, 100)
	f.LogChan["error"] = make(chan string, 100)
	f.LogChan["warning"] = make(chan string, 100)
	f.LogChan["transport"] = make(chan string, 100)
	f.LogChan["thread"] = make(chan string, 100)
	f.LogChan["debug"] = make(chan string, 100)
	go ignoreLogWorker(f.LockLog)
	go printLogWorker(f.LogChan["file"])
	go printLogWorker(f.LogChan["database"])
	go printLogWorker(f.LogChan["error"])
	go printLogWorker(f.LogChan["warning"])
	go ignoreLogWorker(f.LogChan["transport"])
	go ignoreLogWorker(f.LogChan["thread"])
	go ignoreLogWorker(f.LogChan["debug"])
	f.recordCh = inputchan
	f.permanentStoreCh = permanentStoreCh
	f.location = location
    os.MkdirAll(f.location, 0777)
	f.silos = []*tagSilo{}
	f.memory_only = memory_only
	f.maxSilos = number_of_silos
	f.temporary = isTemporary
	f.maxRecords = maxRecords
	f.checkpointMutex = sync.Mutex{}

	if !f.temporary {
		for i := 0; i < number_of_silos; i++ {
			aSilo := createSilo(memory_only, f.maxSilos, fmt.Sprintf("%v", i), 10, f.recordCh, location, permanentStoreCh, isTemporary, f.maxRecords, f.checkpointMutex, f.LogChan)
			aSilo.LockLog = f.LockLog
			aSilo.LogChan = f.LogChan
			//aSilo.test() FIXME
			f.silos = append(f.silos, aSilo)
		}
	}
	go monitorSilosWorker(&f)

	return &f
}

func (f *Farm) SubmitRecord(r RecordTransmittable) {
	f.recordCh <- r
}

func equalPrints(s1, s2 []string) bool {
	sort.Strings(s1)
	sort.Strings(s2)
	if len(s1) != len(s2) {
		return false
	}
	for i, _ := range s1 {
		if s1[i] != s2[i] {
			return false
		}
	}
	return true
}

func (f *Farm) scanFileDatabase(searchString string, maxResults int, exactMatch bool) []ResultRecordTransmittable {
	results := ResultRecordTransmittableCollection{}
	resLock := sync.Mutex{}
	resLock.Lock()
	pending := 0
	for i, aSilo := range f.silos {
		if debug {
			log.Printf("Searching Silo: %v - %v", f.location, i)
		}
		pending = pending + 1
		go func(aSilo *tagSilo) {
			if debug {
				log.Printf("Starting search\n")
			}
			aFing := aSilo.makeFingerprintFromSearch(fmt.Sprintf("%v", searchString))
			if debug {
				log.Printf("Searching with fingerprint: %V", aFing)
			}
			res := aSilo.resultsToTransmittable(aSilo.scanFileDatabase(aFing, maxResults, exactMatch))
			resLock.Lock()
			defer resLock.Unlock()
			for _, r := range res {
					if ! IsIn(r, results) {
						results = append(results, r)
			sort.Sort(results)
			if results.Len() > maxResults {
				results = results[0 : maxResults-1]
					}
				}
			}

			pending = pending - 1
		}(aSilo)
	}

	resLock.Unlock()
	for i := 0; pending > 0; i = i + 1 {
		time.Sleep(10.0 * time.Millisecond)
	}
	return results
}
