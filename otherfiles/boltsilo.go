// silo
package tagbrowser

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/boltdb/coalescer"
	"github.com/tchap/go-patricia/patricia"
)

func (s *tagSilo) predictString(aStr string, maxResults int) []string {
	//var searchRegex = regexp.MustCompile(fmt.Sprintf("^%v", aStr))
	var searchResults = []string{}
	matchedWords := func(prefix patricia.Prefix, item patricia.Item) error {
		if len(searchResults) < maxResults+1 {
			log.Println("Found match for ", aStr, " : ", s.getString(item.(int)))
			searchResults = append(searchResults, s.getString(item.(int)))
			return nil
		} else {
			return fmt.Errorf("Max results exceeded")
		}
	}
	prefix := patricia.Prefix(aStr)
	s.string_table.VisitSubtree(prefix, matchedWords)

	return searchResults
}

func (s *tagSilo) tagToRecordIDs(tagID int) []int {
	if cache_val, ok := s.tag_cache[tagID]; ok {
		s.count("tag_cache_hit")
		return cache_val
	} else {
		s.count("tag_cache_miss")
		key := []byte(fmt.Sprintf("%v", tagID))
		retval := []int{}
		s.count("boltdb_view")
		s.dbHandle.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("TagToRecordTable"))
			if b == nil {
				log.Println("Failed to get bucket TagToRecordTable")
				panic("Failed to get bucket TagToRecordTable")
			}
			//b = tx.Bucket([]byte("MyBucket"))

			val := b.Get(key)
			err := json.Unmarshal(val, &retval)
			//When adding a record, we attempt to add it to each tag
			//If we add a new tag, then there will be no record in this
			//bucket for the new tag, and returning the empty list is fine
			if debug {
				if err != nil {
					log.Printf("Failed to decode tag to record id list (%v) because %v, data is: %V", key, err, retval)
				}
			}

			return nil
		})
		if retval != nil {
			s.tag_cache[tagID] = retval
		}
		return retval
	}
}

func (s *tagSilo) LockMe() {

	//s.LockLog <- fmt.Sprintln("Attempting lock in silo ", s.id)
	s.writeMutex.Lock()
	s.LockLog <- fmt.Sprintln("Got lock in silo ", s.id)

}
func (s *tagSilo) UnlockMe() {
	s.writeMutex.Unlock()

	s.LockLog <- fmt.Sprintln("Released lock in silo ", s.id)

}
func (s *tagSilo) SubmitRecord(r record) {

	if s.memory_db {

		s.LogChan["transport"] <- fmt.Sprintln("SubmitRecord writing to recordCh")

		s.recordCh <- r
	} else {

		s.LogChan["transport"] <- fmt.Sprintln("SubmitRecord writing to fileRecordCh")

		s.recordCh <- r
	}

}

func (s *tagSilo) storeRecordWorker() {
	for i := 0; s == nil; i = i + 1 {
		time.Sleep(time.Millisecond * 100)
	}
	for aRecord := range s.InputRecordCh {
		if s.ReadOnly || !s.Operational {
			//FIXME race condition against shutdown (monitorworker)
			s.InputRecordCh <- aRecord
			s.LogChan["thread"] <- fmt.Sprintln("StoreRecordWorker exiting in silo ", s.id)
			return
		}
		r := record{s.get_or_create_symbol(aRecord.Filename), aRecord.Line, s.makeFingerprint(aRecord.Fingerprint)}
		s.LogChan["transport"] <- fmt.Sprintln("storeRecord writing to recordCh")
		s.recordCh <- r
		s.LogChan["transport"] <- fmt.Sprintln("storeRecord waiting for input")
	}
}

func (s *tagSilo) storePermanentRecordWorker() {
	for aRecord := range s.permanentStoreCh {
		if s.ReadOnly || !s.Operational {
			s.permanentStoreCh <- aRecord
			//FIXME race condition against shutdown (monitorworker)
			log.Println("StorePermanentRecordWorker exiting in silo ", s.id)
			return
		}
		r := record{s.get_or_create_symbol(aRecord.Filename), aRecord.Line, s.makeFingerprint(aRecord.Fingerprint)}
		if debug {
			log.Println("storeRecord writing to recordCh")
		}
		s.recordCh <- r
		if debug {
			log.Println("storeRecord waiting for input")
		}
	}
}

func (s *tagSilo) storeMemRecordWorker() {
	for line_elem := range s.recordCh {
		if s.ReadOnly || !s.Operational {
			//FIXME race condition against shutdown (monitorworker)
			s.recordCh <- line_elem
			log.Println("StoreMemRecordWorker exiting in silo ", s.id)
			return
		}
		//if debug {
		//	log.Println("storeMemRecord got input", line_elem)
		//}

		s.database = append(s.database, line_elem)
		s.last_database_record = len(s.database) - 1

		for _, v := range line_elem.Fingerprint {
			//if !contains(tag2file[v], &line_elem) {
			if debug {
				log.Printf("tag id: %v, tag2file len : %v, string table len : %v", v, len(s.tag2file), len(s.reverse_string_table))
			}
			s.tag2file[v] = append(s.tag2file[v], &s.database[len(s.database)-1])

			if debug {
				log.Printf("Added record to tag %v (%v)", s.getString(v), v)
			}

			//}
		}
		if debug {
			log.Println("storeMemRecord waiting for input")
		}
		s.Dirty()
		if s.last_database_record > s.maxRecords {
			log.Println("Silo ", s.id, " is full ")
			return
		}
	}
}

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

func (s *tagSilo) storeFileRecord(aRecord record) {
	var key []byte

	if debug {
		log.Println("storeFileRecord got input")
	}
	s.last_database_record = s.last_database_record + 1
	key = []byte(fmt.Sprintf("%v", s.last_database_record))
	//if debug {
	//	log.Println("Attempting lock in storeFileRecord")
	//}
	//s.writeMutex.Lock()
	//defer func() { s.writeMutex.Unlock(); log.Println("Released lock in storeFileRecord") }()
	//if debug {
	//	log.Println("Got lock lock in storeFileRecord")
	//}
	//Check to see if the record already exists
	if debug {
		log.Println("Checking for duplicates in storeFileRecord")
	}
	//FIXME this needs to search all silos?  Or do we just filter out dupes during the search consolidation phase?

	//FIXME dupe checks are slowing inserts too much>?
	res := s.scanFileDatabase(searchPrint{aRecord.Fingerprint, fingerPrint{}}, 10, true)
	dupe := false
	if len(res) > 0 {
		for _, v := range res {
			if v.filename == s.getString(aRecord.Filename) && v.line == aRecord.Line {
				dupe = true
			}
		}
	}

	if dupe {
		s.count("duplicates_rejected")
		log.Printf("Attempt to insert a duplicate record.  This is not an error.")
	} else {
		s.Dirty()
		s.count("boltdb_update")

		if debug {
			log.Println("Attempting db update in storeFileRecord")
		}
		s.LockMe()
		defer s.UnlockMe()
		s.coalescer.Update(func(tx *bolt.Tx) error {
			//s.LogChan["transport"] <-fmt.Sprintln("Storing record with ", len(aRecord.Fingerprint), " tags")
			for _, v := range aRecord.Fingerprint {
				//if !contains(tag2file[v], &line_elem) {

				recordIDs := s.tagToRecordIDs(v)
				recordIDs = append(recordIDs, s.last_database_record)

				key := []byte(fmt.Sprintf("%v", v))
				b, err := tx.CreateBucketIfNotExists([]byte("TagToRecordTable"))
				if err != nil {
					log.Println("Could not get bucket TagToRecordTable: ", err)
				}

				val, jerr := json.Marshal(recordIDs)

				if jerr == nil {
					s.count("boltdb_put")
					err = b.Put(key, val)
					s.tag_cache[v] = recordIDs
				} else {
					log.Printf("Failed to marshall json: %v", jerr)
				}
				if err != nil {
					log.Printf("Could not store record for key(%v): %v\n", key, err)
					return err
				}

				if debug {
					log.Printf("Added record to tag %v (%v)", s.getString(v), v)
				}

				//}
			}

			b, err := tx.CreateBucketIfNotExists([]byte("RecordTable"))
			if err != nil {
				log.Println("Could not get bucket RecordTable: ", err)
			}

			val, _ := json.Marshal(aRecord)
			s.count("boltdb_put")
			err = b.Put(key, val)
			s.record_cache[s.last_database_record] = aRecord
			if err != nil {
				log.Printf("Could not store record for key(%v): %v\n", key, err)
			}
			return err
		})

		if debug {
			log.Println("storeFileRecord waiting for input")
		}
	}

}

func (s *tagSilo) storeFileRecordWorker() {

	for aRecord := range s.recordCh {
		if s.ReadOnly || !s.Operational {
			//FIXME race condition against shutdown (monitorworker)
			s.recordCh <- aRecord
			log.Println("StoreFileRecordWorker exiting in silo ", s.id)
			return
		}
		s.storeFileRecord(aRecord)
	}
}

func (s *tagSilo) getRecord(recordID int) record {
	if s.memory_db {
		return s.getMemRecord(recordID)
	} else {
		return s.getDiskRecord(recordID)
	}
}

func (s *tagSilo) getMemRecord(recordID int) record {
	return s.database[recordID]
}

func (s *tagSilo) getDiskRecord(recordID int) record {
	s.count("records_fetched")
	if cache_val, ok := s.record_cache[recordID]; ok {
		s.count("record_cache_hit")
		return cache_val
	} else {
		s.count("record_cache_miss")

		var val []byte
		retval := record{}
		var key []byte
		key = []byte(fmt.Sprintf("%v", recordID))
		var err error
		if s == nil {
			panic("Silo is nil")
		}
		if s.dbHandle == nil {
			panic("nil dbhandle!")
		}
		s.count("boltdb_view")
		s.dbHandle.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("RecordTable"))
			if b == nil {
				log.Println("Failed to get bucket RecordTable")
				return fmt.Errorf("No bucket")
			}
			//b = tx.Bucket([]byte("MyBucket"))

			val = b.Get(key)

			return nil
		})
		if val != nil {

			err = json.Unmarshal(val, &retval)
			if err != nil {
				//time.Sleep(1.0 * time.Second)

				log.Printf("Failed to decode record(%v) because %v, data is: %V", key, err, retval)

				//retval = s.getRecord(recordID)

			}
		}
		if debug {
			log.Printf("Fetched from database: %v\n", retval)
		}
		s.record_cache[recordID] = retval
		return retval
	}
}
func (s *tagSilo) scanFileDatabase(aFing searchPrint, maxResults int, exactMatch bool) resultRecordCollection {
	s.count("database_searches")
	if s.memory_db {
		return s.scanMemDatabase(aFing, maxResults, exactMatch)
	} else {
		return s.scanRealFileDatabase(aFing, maxResults, exactMatch)
	}
}

func (s *tagSilo) scanMemDatabase(aFing searchPrint, maxResults int, exactMatch bool) resultRecordCollection {
	//log.Println("Starting database scan")
	results := resultRecordCollection{}
	currentLowest := 99999
	files := []*record{}
	filesHash := map[string]int{}
	for _, p := range aFing.wanted {
		for _, elem := range s.tag2file[p] {

			files = append(files, elem)
		}
	}

	for _, elem := range files {
		stringKey := fmt.Sprintf("%v:%v", elem.Filename, elem.Line)
		//log.Printf("%v:%v:%v", getString(elem.filename), stringKey, filesHash[stringKey])
		if filesHash[stringKey] < 1 {
			filesHash[stringKey] = 1
			thisScore := s.score(aFing, *elem)
			if thisScore > 0 {
				if !exactMatch || (exactMatch && thisScore == len(aFing.wanted)) {
					if len(results) < maxResults {
						res := resultRecord{s.getString(elem.Filename), elem.Line, elem.Fingerprint, "", thisScore}
						results = append(results, res)
					} else {
						//res := resultRecord{getString(elem.filename), elem.line, elem.fingerprint, "", thisScore}
						//results = append(results, res)
						//sort.Sort(results)
						//results = results[0:11]
						results, currentLowest = s.replaceLowest(results, elem, currentLowest, thisScore)
					}

				}
			}
		}
	}
	//log.Println("Finished database scan")
	//log.Printf("Starting results sort")
	sort.Sort(results)
	//log.Println("Finished results sort")

	//	for index, r := range results {
	//		results[index].sample = getLine(r.filename, r.line)
	//	}
	return results
}

func (s *tagSilo) scanRealFileDatabase(aFing searchPrint, maxResults int, exactMatch bool) resultRecordCollection {

	//defer func() {
	//	if r := recover(); r != nil {
	//		fmt.Println("Recovered in scanfiledatabase:", r)
	//		time.Sleep(1.0 * time.Second)
	//		//return s.scanFileDatabase(aFing, maxResults, exactMatch)
	//	}
	//}()
	//log.Println("Starting database scan")
	results := resultRecordCollection{}
	currentLowest := 99999
	recordIDs := []int{}
	filesHash := map[int]int{}
	for _, p := range aFing.wanted {
		rs := s.tagToRecordIDs(p)
		for _, elem := range rs {

			recordIDs = append(recordIDs, elem)
		}
	}

	for _, recordID := range recordIDs {
		//stringKey := fmt.Sprintf("%v:%v", elem.Filename, elem.Line)
		//log.Printf("%v:%v:%v", getString(elem.filename), stringKey, filesHash[stringKey])
		if filesHash[recordID] < 1 {
			filesHash[recordID] = 1
			record := s.getRecord(recordID)
			thisScore := s.score(aFing, record)
			if thisScore > 0 {
				if !exactMatch || (exactMatch && thisScore == len(aFing.wanted)) {
					if len(results) < maxResults {
						res := resultRecord{s.getString(record.Filename), record.Line, record.Fingerprint, "", thisScore}
						results = append(results, res)
					} else {
						//res := resultRecord{getString(elem.filename), elem.line, elem.fingerprint, "", thisScore}
						//results = append(results, res)
						//sort.Sort(results)
						//results = results[0:11]
						results, currentLowest = s.replaceLowest(results, &record, currentLowest, thisScore)
					}

				}
			}
		}
	}
	//log.Println("Finished database scan")
	//log.Printf("Starting results sort")
	sort.Sort(results)
	//log.Println("Finished results sort")

	//	for index, r := range results {
	//		results[index].sample = getLine(r.filename, r.line)
	//	}
	return results
}

func (s *tagSilo) makeFingerprintFromSearch(aStr string) searchPrint {
	return s.makeSearchPrint(strings.Fields(strings.ToLower(aStr)))
}
func (s *tagSilo) makeFingerprintFromData(aStr string) fingerPrint {
	//seps := []string{"\\\\", "\\.", " ", "\\(", "\\)", "/", "_", "\\b"} //\\b|\\p{Z}+|\\p{C}|\\s+|\\/+|\\.+|\\\\+|_+
	//return makeFingerprint(ReSplit(seps, strings.Fields(strings.ToLower(aStr))))

	return s.makeFingerprint(RegSplit(strings.ToLower(aStr), FragsRegex))
}

func (s *tagSilo) getString(index int) string {
	if s.memory_db {
		if debug {
			log.Println("Fetching string: ", index)
		}
		return s.reverse_string_table[index]
	} else {
		var val string
		if cache_val, ok := s.string_cache[index]; ok {
			s.count("string_cache_hit")
			return cache_val
		} else {
			s.count("string_cache_miss")
			s.count("boltdb_view")
			s.dbHandle.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("StringTable"))
				if b == nil {
					log.Println("Failed to get bucket StringTable")
					return fmt.Errorf("No bucket")
				}
				//b = tx.Bucket([]byte("MyBucket"))

				val = string(b.Get([]byte(fmt.Sprintf("%v", index))))
				return nil
			})
			if val != "" {
				s.string_cache[index] = val
			}
			return val
		}
	}
}

func (s *tagSilo) get_memdb_symbol(aStr string) (int, error) {
	if debug {
		//log.Printf("Silo: %V\n", s)
		log.Printf("string: %V\n", aStr)
	}

	if s == nil {
		panic("Silo is nil")
	}
	if s.dbHandle != nil {
		panic("dbhandle not nil for memdb!")
	}

	key := patricia.Prefix(aStr)
	if key == nil {
		log.Printf("Got nil for radix string lookup on '%v'", aStr)
		log.Printf("Number of Records: %v", len(s.database))
		log.Printf("Number of tags: %v", s.next_string_index+1)
		return 0, fmt.Errorf("Key not found in radix tree")
	}
	defer func() {
		if r := recover(); r != nil {
			log.Println("Error while reading string", r, ", retrying")
			time.Sleep(1 * time.Second)
			read_trie(s.string_table, key)
		}
	}()
	var retval int
	retval = 0
	if match_trie(s.string_table, key) {
		val := s.string_table.Get(key)
		if val == nil {
			log.Printf("Got nil for string table lookup (key:%v)", key)
			log.Printf("Number of Records: %v", len(s.database))
			log.Printf("Number of tags: %v", s.next_string_index+1)
			return 0, fmt.Errorf("String '%s' not found in tag database", aStr)
		}
		retval = val.(int)
	}

	return retval, nil
}

func (s *tagSilo) get_symbol(aStr string) (int, error) {
	var retval int
	var err error
	if val, ok := s.symbol_cache[aStr]; ok {
		s.count("symbol_cache_hit")
		return val, nil
	} else {
		s.count("symbol_cache_miss")
		if s.memory_db {
			retval, err = s.get_memdb_symbol(aStr)
		} else {
			retval, err = s.get_diskdb_symbol(aStr)

		}
		if retval != 0 && err == nil {
			s.symbol_cache[aStr] = retval
		}
	}
	return retval, err
}

func (s *tagSilo) get_diskdb_symbol(aStr string) (int, error) {
	var retval int
	retval = 0
	if debug {
		//log.Printf("Silo: %V\n", s)
		log.Printf("string: %V\n", aStr)
	}
	//log.Printf("Silo: %V, string: %V, db: %V\n", s, aStr, s.dbHandle)
	if s == nil {
		panic("Silo is nil")
	}
	if s.dbHandle == nil {
		panic("nil dbhandle!")
	}
	if s.memory_db {
		key := patricia.Prefix(aStr)
		if key == nil {
			log.Printf("Got nil for radix string lookup on '%v'", aStr)
			log.Printf("Number of Records: %v", len(s.database))
			log.Printf("Number of tags: %v", s.next_string_index+1)
			return 0, fmt.Errorf("Key not found in radix tree")
		}
		defer func() {
			if r := recover(); r != nil {
				log.Println("Error while reading string", r, ", retrying")
				time.Sleep(1 * time.Second)
				read_trie(s.string_table, key)
			}
		}()

		if match_trie(s.string_table, key) {
			val := s.string_table.Get(key)
			if val == nil {
				log.Printf("Got nil for string table lookup (key:%v)", key)
				log.Printf("Number of Records: %v", len(s.database))
				log.Printf("Number of tags: %v", s.next_string_index+1)
				return 0, fmt.Errorf("String '%s' not found in tag database", aStr)
			}
			retval = val.(int)
		}

	} else {

		var res []byte
		s.count("boltdb_view")
		viewErr := s.dbHandle.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("SymbolTable"))
			if b == nil {
				log.Println("Failed to get bucket SymbolTable")
				panic("Failed to get bucket SymbolTable")
			}
			res = b.Get([]byte(aStr))
			if res != nil {
				val, err := strconv.ParseInt(string(res), 10, 0)
				if err != nil {
					log.Println("Could not parse int, because ", err)
				} else {
					retval = int(val)
				}
			}
			return nil
		})
		if viewErr != nil {
			log.Printf("Error retrieving symbol for %v: %v", aStr, viewErr)
		}
	}
	return retval, nil

}

func (s *tagSilo) get_or_create_symbol(aStr string) int {
	for i := 0; s == nil; i = i + 1 {
		time.Sleep(time.Millisecond * 100)
	}
	if val, ok := s.symbol_cache[aStr]; ok {
		s.count("get/create_symbol_cache_hit")
		return val
	} else {
		s.count("get/create_symbol_cache_miss")
		if aStr == "" {
			log.Printf("Invalid insert!  Cannot insert empty string into symbol table")
			log.Printf("Number of Records: %v", len(s.database))
			log.Printf("Number of tags: %v", s.next_string_index+1)
			return 0
		}

		val, _ := s.get_symbol(aStr)
		if val != 0 {
			return val
		} else {

			val, _ := s.get_symbol(aStr)
			if val != 0 {
				return val
			} else {
				s.next_string_index = s.next_string_index + 1
				if !s.memory_db {
					//log.Println("Attempting lock in get_or_create_symbol")
					//s.writeMutex.Lock()
					//defer func() { s.writeMutex.Unlock(); log.Println("Released lock in get_or_create_symbol") }()
					//log.Println("Got lock in get_or_create_symbol")
					s.count("boltdb_update")
					s.coalescer.Update(func(tx *bolt.Tx) error {
						b, err := tx.CreateBucketIfNotExists([]byte("StringTable"))
						s.count("boltdb_put")
						err = b.Put([]byte(fmt.Sprintf("%v", s.next_string_index)), []byte(aStr))

						b, err = tx.CreateBucketIfNotExists([]byte("SymbolTable"))
						s.count("boltdb_put")
						err = b.Put([]byte(aStr), []byte(fmt.Sprintf("%v", s.next_string_index)))

						return err
					})
				} else {
					s.string_table.Insert(patricia.Prefix(aStr), s.next_string_index)
					if s.next_string_index < len(s.reverse_string_table)-1 {
						if debug {
							log.Println("Inserting tag into reverse string table: ", aStr)
						}
						s.reverse_string_table[s.next_string_index] = aStr

					} else {
						if debug {
							log.Println("Extending reverse string table for tag ", aStr)
						}
						s.reverse_string_table = append(s.reverse_string_table, aStr)
					}
				}
				s.last_tag_record = s.last_tag_record + 1
				if debug {
					log.Printf("Storing mem tag %v and disk tag %v\n", s.next_string_index, s.last_tag_record)
				}
				if s.next_string_index < len(s.tag2file)-1 {
					s.tag2file[s.next_string_index] = []*record{}

				} else {
					s.tag2file = append(s.tag2file, []*record{})

				}
				if debug {
					log.Printf("Finished store\n")
				}
				s.Dirty()
				return s.next_string_index
			}
		}
	}
}

func (s *tagSilo) makeSearchPrint(fragments []string) searchPrint {

	//Pull this out into a separate function FIXME
	frags := map[int]int{}
	for _, f := range fragments {
		if len(f) > 1 && len(f) < maxTagLength {

			key, rawScore := calcRawScore(f)
			//fmt.Printf("key: %v, score: %v\n", key, rawScore)
			table_index, err := s.get_symbol(key)
			if err == nil {
				frags[table_index] = rawScore
			}
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

func (s *tagSilo) replaceLowest(resultsList resultRecordCollection, candidate *record, currentLowest int, cScore int) (resultRecordCollection, int) {
	replacePending := true
	for i, v := range resultsList {
		if resultsList[i].score < currentLowest {
			currentLowest = resultsList[i].score
		}
		if v.score == currentLowest && replacePending {
			//log.Printf("Inserting into resultset: %v, %v", getString(candidate.filename), candidate.line)
			resultsList[i].filename = s.getString(candidate.Filename)
			resultsList[i].line = candidate.Line
			resultsList[i].fingerprint = candidate.Fingerprint
			resultsList[i].score = cScore
			replacePending = false
		}

	}
	return resultsList, currentLowest
}

func (s *tagSilo) scanDatabase(aFing searchPrint, maxResults int, exactMatch bool) resultRecordCollection {
	//log.Println("Starting database scan")
	results := resultRecordCollection{}
	currentLowest := 99999
	files := []*record{}
	filesHash := map[string]int{}
	for _, p := range aFing.wanted {
		for _, elem := range s.tag2file[p] {

			files = append(files, elem)
		}
	}

	for _, elem := range files {
		stringKey := fmt.Sprintf("%v:%v", elem.Filename, elem.Line)
		//log.Printf("%v:%v:%v", getString(elem.filename), stringKey, filesHash[stringKey])
		if filesHash[stringKey] < 1 {
			filesHash[stringKey] = 1
			thisScore := s.score(aFing, *elem)
			if thisScore > 0 {
				if !exactMatch || (exactMatch && thisScore == len(aFing.wanted)) {
					if len(results) < maxResults {
						res := resultRecord{s.getString(elem.Filename), elem.Line, elem.Fingerprint, "", thisScore}
						results = append(results, res)
					} else {
						//res := resultRecord{getString(elem.filename), elem.line, elem.fingerprint, "", thisScore}
						//results = append(results, res)
						//sort.Sort(results)
						//results = results[0:11]
						results, currentLowest = s.replaceLowest(results, elem, currentLowest, thisScore)
					}

				}
			}
		}
	}
	//log.Println("Finished database scan")
	//log.Printf("Starting results sort")
	sort.Sort(results)
	//log.Println("Finished results sort")

	//	for index, r := range results {
	//		results[index].sample = getLine(r.filename, r.line)
	//	}
	return results
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

func (s *tagSilo) makeFingerprint(fragments []string) fingerPrint {
	frags := map[int]int{}

	index := 0
	for _, f := range fragments {

		if len(f) > 1 && len(f) < maxTagLength {

			key, _ := calcRawScore(f)
			//log.Printf("Key: %v", key)
			table_index := s.get_or_create_symbol(key)
			_, ok := frags[table_index]
			if !ok {
				frags[table_index] = index
				index = index + 1
			}
		} else {
			//s.LogChan["error"] <-fmt.Sprintln("Rejecting tag as too long or short: ", f)
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

func (s *tagSilo) score(a searchPrint, b record) int {
	score := 0
	//We can do much better than a nested loop here

	for _, vv := range b.Fingerprint {
		for _, v := range a.wanted {
			if v == vv {
				score += 1
				if debug {
					fmt.Println(b.Filename)

					fmt.Printf("\nWanted ")
					s.dumpFingerprint(a.wanted)
					fmt.Printf("\nUnwanted ")
					s.dumpFingerprint(a.unwanted)
					fmt.Printf("\nsearch ")
					s.dumpFingerprint(b.Fingerprint)
					fmt.Printf("Matched %v and %v, wanted, score is now %v\n", s.getString(v), s.getString(vv), score)
				}
			}

		}
		for _, v := range a.unwanted {
			if v == vv {
				score -= 1
				if debug {
					fmt.Println(b.Filename)

					fmt.Printf("\nWanted ")
					s.dumpFingerprint(a.wanted)
					fmt.Printf("\nUnwanted ")
					s.dumpFingerprint(a.unwanted)
					fmt.Printf("\nsearch ")
					s.dumpFingerprint(b.Fingerprint)
					fmt.Printf("Matched %v and %v, unwanted, score is now %v\n", s.getString(v), s.getString(vv), score)
				}
			}

		}
	}
	//fmt.Println("----")
	return score
}

func (s *tagSilo) offloadWorker() {
	for {
		if !s.Operational {
			return
		}
		//log.Printf("Checking offloads in silo %v.  offload index: %v, database record index: %v", s.id, s.offload_index, s.last_database_record)
		if s.offloading && s.offload_index < s.last_database_record {

			s.LogChan["transport"] <- fmt.Sprintf("silo %v, offload index is %v, last database record is %v, offloading now", s.id, s.offload_index, s.last_database_record)

			s.offload_index = s.offload_index + 1
			r := s.getRecord(s.offload_index)
			strings := []string{}
			for _, v := range r.Fingerprint {
				//fmt.Printf("k: %v, v: %v\n", k, v)
				strings = append(strings, s.getString(v))
			}
			rTrans := RecordTransmittable{s.getString(r.Filename), r.Line, strings}
			if debug {
				s.LogChan["transport"] <- fmt.Sprintln("Offloading record ", rTrans)
			}
			s.permanentStoreCh <- rTrans
		} else {
			time.Sleep(100.0 * time.Millisecond)
		}
	}
}
func (s *tagSilo) monitorSiloWorker() {
	for i := 0; i < 1; i = 0 {
		time.Sleep(time.Second * 60.0)
		s.count("minutes")
		if s.temporary {
			if s.last_database_record > s.maxRecords {
				s.ReadOnly = true
			}
			if s.ReadOnly && s.offload_index == s.last_database_record {
				//Fixme, race condition against the worker threads
				log.Println(s.id, " moving to shutdown in 20 seconds")
				time.Sleep(time.Second * 20.0)
				s.Operational = false
				log.Println(s.id, " no longer operational")
				return
			}
		}
		if len(s.string_cache) > 10000 {
			s.string_cache = map[int]string{}
			s.count("string_cache_clear")
		}
		if len(s.symbol_cache) > 10000 {
			s.symbol_cache = map[string]int{}
			s.count("symbol_cache_clear")
		}

		if len(s.tag_cache) > 10000 {
			s.tag_cache = map[int][]int{}
			s.count("string_cache_clear")
		}

		log.Println("Silo: ", s.filename, " : ", s.id, s.counters)

	}
}

func (s *tagSilo) count(name string) {
	s.counterMutex.Lock()
	defer s.counterMutex.Unlock()
	s.counters[name] = s.counters[name] + 1
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
	//String_table      *patricia.Trie

	Reverse_string_table []string
	Tag2file             [][]*record
	Tag2record           [][]int

	Temporary     bool
	Offload_index int
	Offloading    bool
	MaxRecords    int
}

func (s *tagSilo) Dirty() {
	s.LockLog <- "Taking dirty lock"
	s.checkpointMutex.Lock()
	defer s.checkpointMutex.Unlock()
	s.dirty = true
	s.LockLog <- "Released dirty lock"
}
func (s *tagSilo) Checkpoint() {
	s.LockLog <- "Locking checkpoint"
	s.checkpointMutex.Lock()
	defer s.checkpointMutex.Unlock()

	s.LockMe()
	defer s.UnlockMe()
	s.LogChan["file"] <- fmt.Sprintf("Checkpointing silo %v", s.id)
	d := SerialiseMe{s.id, s.last_database_record, s.database, s.counters, s.next_string_index, s.last_tag_record, s.reverse_string_table, s.tag2file, s.tag2record, s.temporary, s.offload_index, s.offloading, s.maxRecords}

	f, _ := os.Create(fmt.Sprintf("%v.checkpoint", s.filename))

	enc := gob.NewEncoder(f)
	encErr := enc.Encode(d)
	if encErr != nil {
		log.Println("Failed to checkpoint silo: ", encErr)
	}
	f.Sync()
	f.Close()
	s.dirty = false
	s.LockLog <- "Released checkpoint lock"
}

func (s *tagSilo) checkpointWorker() {
	for {
		time.Sleep(time.Second * 300.0)
		if s.dirty && s.Operational {
			s.Checkpoint()
			//log.Println("Checkpoint complete ", s.filename)
		}
		if !s.Operational {
			return
		}
	}
}

func createSilo(memory bool, preAllocSize int, id string, channel_buffer int, inputChan chan RecordTransmittable, dataDir string, permanentStoreCh chan RecordTransmittable, isTemporary bool, maxRecords int, checkpointMutex sync.Mutex, logChans map[string]chan string) *tagSilo {

	silo := tagSilo{}
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
	silo.string_cache = map[int]string{}
	silo.symbol_cache = map[string]int{}
	silo.tag_cache = map[int][]int{}
	silo.record_cache = map[int]record{}
	silo.counters = map[string]int{}
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
				silo.counters = d.Counters
				silo.next_string_index = d.Next_string_index
				silo.last_tag_record = d.Last_tag_record
				//silo.string_table = d.String_table

				silo.reverse_string_table = d.Reverse_string_table
				silo.tag2file = d.Tag2file
				silo.tag2record = d.Tag2record

				//s.temporary     = d.Temporary
				silo.offload_index = d.Offload_index
				//s.offloading    = d.Offloading

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

		log.Printf("Opening silo %v", silo.filename)
		var err error
		silo.dbHandle, err = bolt.Open(silo.filename, 0600, nil)
		log.Println("Opened file")
		//silo.dbHandle.NoSync = true
		if err != nil {
			log.Fatal(err)
		}

		c, cerr := coalescer.New(silo.dbHandle, 10, 100.0*time.Millisecond)
		if cerr != nil {
			log.Println("Could not create coalescer")
			os.Exit(1)
		}
		silo.coalescer = c

		silo.coalescer.Update(func(tx *bolt.Tx) error {
			silo.LogChan["file"] <- fmt.Sprintln("Setting up bolt database")
			stringBucket, err := tx.CreateBucketIfNotExists([]byte("StringTable"))
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintf("Error creating buckets: %v", err)
			}
			_, err = tx.CreateBucketIfNotExists([]byte("SymbolTable"))
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintf("Error creating buckets: %v", err)
			}
			var recordBucket *bolt.Bucket
			recordBucket, err = tx.CreateBucketIfNotExists([]byte("RecordTable"))
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintf("Error creating buckets: %v", err)
			}
			_, err = tx.CreateBucketIfNotExists([]byte("TagToRecordTable"))
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintf("Error creating buckets: %v", err)
			}
			//log.Println("Buckets created")
			//We should store this properly
			recordBucket.ForEach(func(k, v []byte) error {
				if k != nil {
					val, err := strconv.ParseInt(string(k), 10, 0)
					if err != nil {
						silo.LogChan["error"] <- fmt.Sprintln("Could not parse int, because ", err)
					} else {
						if int(val) > silo.last_database_record {
							silo.last_database_record = int(val)
						}
					}
				}

				return nil
			})
			//We should store this properly
			stringBucket.ForEach(func(k, v []byte) error {
				if k != nil {
					val, err := strconv.ParseInt(string(k), 10, 0)
					if err != nil {
						silo.LogChan["error"] <- fmt.Sprintln("Could not parse int, because ", err)
					} else {
						if int(val) > silo.next_string_index {
							silo.next_string_index = int(val)
						}
					}
				}

				return nil
			})

			return err
		})
		go silo.storeFileRecordWorker()
	}
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
	}

	silo.Operational = true
	s := &silo
	silo.threadsWait.Add(1)
	go s.monitorSiloWorker()

	silo.LogChan["file"] <- fmt.Sprintln("Silo operational, ", len(silo.reverse_string_table), " entries,  ", silo.next_string_index, " strings, ", silo.last_tag_record, " tags")

	return &silo
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
