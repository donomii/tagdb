// silo
package tagbrowser

import (
	//"runtime/pprof"
	//debugModule "runtime/debug"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"time"

	lsmkv "./lsmkv"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
)

type WeaviateStore struct {
	StringTable      *lsmkv.Bucket
	SymbolTable      *lsmkv.Bucket
	RecordTable      *lsmkv.Bucket
	TagToRecordTable *lsmkv.Bucket
	DirectoryName    string
}

// NewNullLogger creates a discarding logger and installs the test hook.
func NewNullLogger() *logrus.Logger {

	logger := logrus.New()
	logger.Out = ioutil.Discard

	return logger

}

func NewWeaviateStore(dirname string) *WeaviateStore {
	n := NewNullLogger()
	s := WeaviateStore{
		StringTable:      lsmkv.MustNewBucket(context.Background(), dirname+"stringtable", "", n, nil, nil, lsmkv.WithStrategy(lsmkv.StrategyReplace)),
		SymbolTable:      lsmkv.MustNewBucket(context.Background(), dirname+"symboltable", "", n, nil, nil, lsmkv.WithStrategy(lsmkv.StrategyReplace)),
		RecordTable:      lsmkv.MustNewBucket(context.Background(), dirname+"recordtable", "", n, nil, nil, lsmkv.WithStrategy(lsmkv.StrategyReplace)),
		TagToRecordTable: lsmkv.MustNewBucket(context.Background(), dirname+"tagtorecord", "", n, nil, nil, lsmkv.WithStrategy(lsmkv.StrategyRoaringSet)),
		DirectoryName:    dirname,
	}

	return &s
}

//Works like printf, but outputs to log
func Debugf(format string, args ...interface{}) {
	if debug {
		log.Printf(format, args...)
	}
}

//Works like println, but outputs to log
func Debugln(args ...interface{}) {
	if debug {
		log.Println(args...)
	}
}

type SiloMetaData struct {
	Next_string_index     uint64
	Last_database_record  uint64
	last_database_tag     uint64
	last_database_tag_key uint64
}

func (s *WeaviateStore) Init(silo *tagSilo) {

	Debugln("Initialising silo ", silo.id)

	data, err := ioutil.ReadFile(s.DirectoryName + "metadata")
	fmt.Printf("silo: %v\n", silo)
	metadata := SiloMetaData{}
	err = json.Unmarshal(data, &metadata)
	silo.next_string_index = s.StringTable.Count() + 1
	silo.last_database_record = s.RecordTable.Count() + 1
	if err != nil {
		silo.LogChan["error"] <- fmt.Sprintf("Reading silo metadata: %v", err)
		silo.next_string_index = 1
		silo.last_database_record = 1
	}

	Debugln("Buckets created")
	//We should store this properly
	Debugln("Set next_string_index to ", silo.next_string_index)
	Debugln("Set last_database_record to ", silo.last_database_record)

	go silo.storeFileRecordWorker()
	//Every minute, Flush the database
	go func() {
		for {
			s.Flush(silo)
			time.Sleep(1 * time.Minute)
		}
	}()
	log.Println("Initialised silo ", silo.id)
}

func (store *WeaviateStore) GetString(s *tagSilo, index int) string {
	s.count("string_cache_miss")
	s.count("sql_select")

	val_b, err := store.StringTable.Get([]byte(fmt.Sprintf("%d", index)))
	if err != nil {
		s.LogChan["warning"] <- fmt.Sprintln("While trying to read StringTable: ", err)
	}

	val := string(val_b)
	if val != "" {
		s.string_cache.Store(index, val)
	}
	return val
}

func (store *WeaviateStore) Dbh() *sql.DB {

	return nil
}

func (s *WeaviateStore) GetSymbol(silo *tagSilo, aStr string) int {
	silo.count("sql_select")

	res, err := s.SymbolTable.Get([]byte(aStr))
	var retval int
	if err == nil {
		retval, _ = strconv.Atoi(string(res))
	} else {
		Debugf("Error retrieving symbol for '%v': %v", aStr, err)
		return 0 //Note that 0 is "no symbol"
	}
	Debugf("Retrieved symbol for '%v': %v", aStr, retval)

	return retval
}

func (s *WeaviateStore) InsertRecord(silo *tagSilo, key []byte, aRecord record) {
	val, _ := json.Marshal(aRecord)

	err := s.RecordTable.Put(key, val)

	if err != nil {
		silo.LogChan["warning"] <- fmt.Sprintf("While trying to insert RecordTable: %v", err)
		silo.LogChan["warning"] <- fmt.Sprintf("Could not store record for key(%v): %v\n", key, err)
		return
	}

	silo.count("weaviate_insert")
	silo.record_cache.Store(silo.last_database_record, aRecord)
	Debugf("Record %v inserted: %v", silo.last_database_record, string(val))
}

func (s *WeaviateStore) GetRecord(key []byte) record {

	val, err := s.RecordTable.Get(key)
	var retval record

	if val != nil {
		err = json.Unmarshal(val, &retval)
		if err != nil {
			panic("Could not retrieve record")
		}
	}
	Debugf("Fetched from database: %v\n", retval)

	return retval
}

func (s *WeaviateStore) InsertStringAndSymbol(silo *tagSilo, aStr string) {
	silo.count("weaviate_insert")

	err := s.StringTable.Put([]byte(fmt.Sprintf("%d", silo.next_string_index)), []byte(aStr))

	//log.Printf("insert into StringTable(id, value) values(%v, %s)\n",silo.next_string_index, aStr)
	if err != nil {
		silo.LogChan["error"] <- fmt.Sprintln("While trying to insert ", aStr, " into StringTable as ", silo.next_string_index, " into ", silo.id, ": ", err)
	}

	silo.count("sql_insert")

	s.SymbolTable.Put([]byte(aStr), []byte(fmt.Sprintf("%d", silo.next_string_index)))
	if err != nil {
		silo.LogChan["error"] <- fmt.Sprintln("While trying to insert ", aStr, " into SymbolTable: ", err)
	}
}

func (s *WeaviateStore) Flush(silo *tagSilo) {

	silo.LogChan["warning"] <- fmt.Sprintf("Flushing silo %v", silo.id)
	//log.Printf("Flushing silo %v\n", silo.id)
	s.StringTable.FlushMemtable()
	s.StringTable.Compact()
	s.SymbolTable.FlushMemtable()
	s.SymbolTable.Compact()
	s.RecordTable.FlushMemtable()
	s.RecordTable.Compact()
	s.TagToRecordTable.FlushMemtable()
	s.TagToRecordTable.Compact()
	silo.LogChan["warning"] <- fmt.Sprintf("Flushed silo %v", silo.id)
	//log.Printf("Flushed silo %v\n", silo.id)
	smd := SiloMetaData{
		Next_string_index:    uint64(silo.next_string_index),
		Last_database_record: uint64(silo.last_database_record),
	}
	data, err := json.Marshal(smd)
	if err != nil {
		silo.LogChan["error"] <- fmt.Sprintf("While trying to marshal metadata: %v", err)
		return
	}

	ioutil.WriteFile(s.DirectoryName+"metadata", data, 0644)
}

func (s *WeaviateStore) GetRecordId(tagID int) []int {
	var retarr []int
	key_bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(key_bytes, uint32(tagID))

	bm, err := s.TagToRecordTable.RoaringSetGet(key_bytes)
	if err != nil {
		log.Println("While trying to retrieve TagToRecord: ", err)
	}

	for _, v := range bm.ToArray() {
		retarr = append(retarr, int(v))
	}

	//log.Printf("GetRecordId: Would return %v\n", retarr)
	return retarr
}

func (s *WeaviateStore) StoreTagToRecord(recordId int, fp fingerPrint) {
	for _, key := range fp {
		key_bytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(key_bytes, uint32(key))

		err := s.TagToRecordTable.RoaringSetAddOne(key_bytes, uint64(recordId))
		if err != nil {
			log.Println("While trying to insert TagToRecord: ", err)
		}
	}
}

func (s *WeaviateStore) StoreRecordId(key []byte, val []byte) {
	panic("Don't use this")
}
