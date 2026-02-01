// manor.go

//A manor holds several farms. The manor accepts records to be stored on one of the farms,
//also it searches all the farms during a query, then combines the results and returns them

package tagbrowser

import (
	"log"
	"sort"
	"sync"
)

type Manor struct {
	Farms            []*Farm
	recordCh         chan RecordTransmittable //Used to send records to all the farms
	permanentStoreCh chan RecordTransmittable //Used to send records to disk databases only
}

func CreateManor(config tomlConfig) *Manor {
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
	log.Printf("Requesting %v results\n", maxResults)
	results := ResultRecordTransmittableCollection{}
	resLock := sync.Mutex{}
	resLock.Lock()
	var wg sync.WaitGroup
	log.Printf("Searching %v farms: %v", len(m.Farms), m.Farms)
	for _, aFarm := range m.Farms {
		if debug {
			log.Printf("Searching Farm: %v", aFarm.location)
		}
		wg.Add(1)
		go func(threadFarm *Farm) {
			defer wg.Done()
			res := threadFarm.scanFileDatabase(searchString, maxResults, exactMatch)
			resLock.Lock()
			defer resLock.Unlock()
			if debug {
				log.Printf("Merging in resultset %v for farm %v", res, threadFarm.location)
			}
			for _, r := range res {
				if !IsIn(r, results) {
					results = append(results, r)
					sort.Sort(results)
					if results.Len() > maxResults {
						results = results[0:maxResults]
					}
				}
			}
		}(aFarm)
	}
	wg.Wait()
	sort.Sort(results)

	return results
}
