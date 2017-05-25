// manor.go

//A manor holds several farms. The manor accepts records to be stored on one of the farms,
//also it searches all the farms during a query, then combines the results and returns them

package tagbrowser

import (
	"log"
	"sort"
	"sync"
	"time"
)

type Manor struct {
	Farms            []*Farm
	recordCh         chan RecordTransmittable //Used to send records to all the farms
	permanentStoreCh chan RecordTransmittable //Used to send records to disk databases only
}

func createManor(config tomlConfig) *Manor {
	m := Manor{}
	m.Farms = []*Farm{}
	m.recordCh = make(chan RecordTransmittable, 100)
	m.permanentStoreCh = make(chan RecordTransmittable, 100)

	for _, v := range config.Farms {
		var mem bool
		if v.Mode == "memory" {
			mem = true
		}
		log.Printf("Creating farm at %v, %v silos, memory only: %v, offloading: %v", v.Location, v.Silos, mem, v.Offload)
		f := createFarm(v.Location, v.Silos, m.recordCh, mem, m.permanentStoreCh, v.Offload, v.Size)
		m.Farms = append(m.Farms, f)
	}
	return &m
}

func (m *Manor) Shutdown() {
	for _, f := range m.Farms {
		f.Shutdown()
	}
}

func (m *Manor) SubmitRecord(r RecordTransmittable) {
    if debug {
        log.Println("Submitting record")
    }
	m.recordCh <- r
    if debug {
        log.Println("Record submitted")
    }
}

func (m *Manor) scanFileDatabase(searchString string, maxResults int, exactMatch bool) []ResultRecordTransmittable {
	results := ResultRecordTransmittableCollection{}
	resLock := sync.Mutex{}
	resLock.Lock()
	pending := 0
	for _, aFarm := range m.Farms {
		if debug {
			log.Printf("Searching Farm: %v", aFarm.location)
		}
		pending = pending + 1
		go func() {
			res := aFarm.scanFileDatabase(searchString, maxResults, exactMatch)
			resLock.Lock()
			defer resLock.Unlock()
			for _, r := range res {
				temp := IsIn(r, results)
				if !temp {
					results = append(results, r)
				}
			}
			sort.Sort(results)
			if results.Len() > maxResults {
				results = results[0 : maxResults-1]
			}
			pending = pending - 1
		}()
	}
	resLock.Unlock()
	for i := 0; pending > 0; i = i + 1 {
		time.Sleep(1.0 * time.Millisecond)
	}

	return results
}
