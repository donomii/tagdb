// tagTypes.go
package tagbrowser

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/gob"
	"sync"

	"github.com/tchap/go-patricia/patricia"
)

var ServerAddress = "127.0.0.1:6781"
var RpcPath = "/rpc/json/"

//RPC
type Args struct {
	A     string
	Limit int
}

type Reply struct {
	C []ResultRecordTransmittable
}

type StringListReply struct {
	C []string
}

type InsertArgs struct {
	Name     string
	Position int
	Tags     []string
}

type SuccessReply struct {
	Success bool
	Reason  string
}

type StatusReply struct {
	Answer map[string]string
}

type HistoReply struct {
	TagsToFilesHisto map[string]int //Show how many files each tag points to
}

type TopTagsReply struct {
	TopTags map[string]int //The number of files the top 10 tags point to
}

type TagResponder int

//Internal database

type tagSilo struct {
	memory_db            bool
	id                   string
	last_database_record int
	filename             string
	dbHandle             *sql.DB
	transactionHandle    driver.Tx
	recordCh             chan record
	permanentStoreCh     chan RecordTransmittable
	InputRecordCh        chan RecordTransmittable
	database             []record //The in memory database, if any
	writeMutex           sync.Mutex
	readMutex            sync.Mutex
	trieMutex            sync.Mutex
	counterMutex         sync.Mutex
	checkpointMutex      sync.Mutex
	counters             map[string]int
	next_string_index    int
	last_tag_record      int
	string_table         *patricia.Trie

	reverse_string_table []string
	tag2file             [][]*record
	tag2record           [][]int
	anotherbuffer        *bytes.Buffer
	dec                  *gob.Decoder
	temporary            bool
	offload_index        int
	offloading           bool
	maxRecords           int
	Operational          bool
	ReadOnly             bool
	string_cache         map[int]string
	symbol_cache         map[string]int
	tag_cache            map[int][]int
	record_cache         map[int]record
	threadsWait          sync.WaitGroup
	dirty                bool
	LockLog              chan string
	LogChan              map[string]chan string
}

type tomlConfig struct {
	Server server `toml:"database"`
	Farms  map[string]serverInfo
}

type server struct {
	Server  string
	Ports   []int
	ConnMax int `toml:"connection_max"`
	Enabled bool
}

type serverInfo struct {
	Location string //Directory to store silos in.  Ignored for memory databases, but useful for debugging messages
	Silos    int    //Maximum number of silos in this farm
	Mode     string //"memory" or "disk"
	Offload  bool   //Should the farm manager automatically move data out of these silos?
	Size     int    //Maximum number of records to store in a silo.  Ignored for disk DBs
}

type fingerPrint []int

type searchPrint struct {
	wanted   fingerPrint
	unwanted fingerPrint
}

type record struct {
	Filename    int
	Line        int
	Fingerprint fingerPrint
}

type RecordTransmittable struct {
	Filename    string
	Line        int
	Fingerprint []string
}

type resultRecord struct {
	filename    string
	line        int
	fingerprint fingerPrint
	sample      string
	score       int
}

type ResultRecordTransmittable struct {
	Filename    string
	Line        string
	Fingerprint []string
	Sample      string
	Score       string
}

type resultRecordCollection []resultRecord
type ResultRecordTransmittableCollection []ResultRecordTransmittable
