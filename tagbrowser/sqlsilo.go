// silo
package tagbrowser

import (
    //"runtime/pprof"
    //debugModule "runtime/debug"
    "database/sql"
	"encoding/json"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"

)

func NewSQLStore(filename string) *SqlStore {
    s := SqlStore{}
    db, err := sql.Open("sqlite3", filename)
    if err != nil {
        log.Fatal(err)
    }
    s.Db = db
    return &s
}

//FIXME this needs to go, all access must be through member functions!
func (s *SqlStore) Dbh () *sql.DB {
    return s.Db
}

func (s *SqlStore) Init(silo *tagSilo) {
        if debug {
            log.Println("Initialising silo ", silo.id)
        }
		sqlStmt := `PRAGMA synchronous = OFF;
		PRAGMA journal_mode = MEMORY;
		`
		var err error
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Performing PRAGMA - %q: %s\n", err, sqlStmt)
		}
		//sqlStmt = `create table IF NOT EXISTS TagToRecord (tagid int not null, recordid int not null, UNIQUE(tagid, recordid));`
		sqlStmt = `create table IF NOT EXISTS TagToRecord (tagid int not null, recordid int not null);`
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Creating TagToRecord - %q: %s\n", err, sqlStmt)
		}


		sqlStmt = `create table IF NOT EXISTS StringTable (id int not null primary key, value string not null);`
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Creating StringTable - %q: %s\n", err, sqlStmt)
		}

		sqlStmt = `create table IF NOT EXISTS SymbolTable (id string not null primary key, value int not null);`
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Creating SymbolTable - %q: %s\n", err, sqlStmt)
		}

		sqlStmt = `create table IF NOT EXISTS RecordTable (id int not null primary key, value blob not null);`
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Creating RecordTable - %q: %s\n", err, sqlStmt)
		}

		sqlStmt = `create table IF NOT EXISTS TagToRecordTable (id int not null primary key, value int not null);`
		_, err = s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Creating TagToRecordTable - %q: %s\n", err, sqlStmt)
		}

		rows, err := s.Db.Query("select id, value from RecordTable")
		if err != nil {
			silo.LogChan["database"] <- fmt.Sprintf("Reading from table RecordTable", err)
		}

		for rows.Next() {
			var k int
			var name string
			rows.Scan(&k, &name)
			var val = k
			//val, err := strconv.ParseInt(string(k), 10, 0)
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintln("Could not parse int, because ", err)
			} else {
				if int(val) > silo.last_database_record {
					silo.last_database_record = int(val)
				}
			}

		}
		rows.Close()

		rows, err = s.Db.Query("select id from StringTable")
		if err != nil {
			silo.LogChan["database"] <- fmt.Sprintf("Reading from table StringTable", err)
		}

		for rows.Next() {
			var k int
			rows.Scan(&k)
			if err != nil {
				silo.LogChan["error"] <- fmt.Sprintln("Could not parse int, because ", err)
			} else {
				if k > silo.next_string_index {
					silo.next_string_index = k
				}
			}

		}
		rows.Close()
        if debug {
            log.Println("Buckets created")
            //We should store this properly
            log.Println("Set next_sting_index to ", silo.next_string_index)
        }

		go silo.storeFileRecordWorker()
        if debug {
            log.Println("Initialised silo ", silo.id)
        }
}

func (store *SqlStore) GetString(s *tagSilo, index int) string {
    var val string
    s.count("string_cache_miss")
    s.count("sql_select")

    err := store.Db.QueryRow("select value from StringTable where id like ?", index).Scan(&val)
    if err != nil {
        //s.LogChan["warning"] <- fmt.Sprintln("While trying to read StringTable: ", err)
    }

    if val != "" {
        s.string_cache[index] = val
    }
    return val
}

func (s *SqlStore) GetSymbol(silo *tagSilo, aStr string) int {
    silo.count("sql_select")
    var res int
    err := s.Db.QueryRow("select value from SymbolTable where id like ?", []byte(aStr)).Scan(&res)
    if err != nil {
        //s.LogChan["warning"] <- fmt.Sprintln("While trying to read  ", aStr, " from SymbolTable: ", err)
    }
    var retval int
    if err == nil {
        retval = int(res)
    } else {
        if debug {
            log.Printf("Error retrieving symbol for '%v': %v", aStr, err)
        }
        return 0  //Note that 0 is "no symbol"
    }
    if debug {
        log.Printf("Retrieved symbol for '%v': %v", aStr, retval)
    }
    return retval
}


func (s *SqlStore) InsertRecord(silo *tagSilo, key []byte, aRecord record) {
    val, _ := json.Marshal(aRecord)
    stmt, err := s.Db.Prepare("insert into RecordTable(id, value) values(?, ?)")
    if err != nil {
        silo.LogChan["error"] <- fmt.Sprintln("While preparing to insert RecordTable: ", err)
        return
    }
    /*
        stmt1, err1 := s.Db.Prepare("update RecordTable SET value = ? where id like ? ")
        if err1 != nil {
            s.LogChan["error"] <- fmt.Sprintln("While preparing to update RecordTable: ", err1)
        }
        _, err1 = stmt1.Exec(val, key)
        if err1 != nil {
            s.LogChan["error"] <- fmt.Sprintln("While trying to insert RecordTable: ", err1)
        }
        defer stmt1.Close()
    */

    defer stmt.Close()
    if debug {
        log.Printf("insert into RecordTable(id, value) values(%v, %v)\n", key, val)
    }
    _, err = stmt.Exec(key, val)

    if err != nil {
        silo.LogChan["warning"] <- fmt.Sprintln("While trying to insert RecordTable: ", err)
        silo.LogChan["warning"] <- fmt.Sprintln("Could not store record for key(%v): %v\n", key, err)
        return
    }

    silo.count("sql_insert")
    silo.record_cache[silo.last_database_record] = aRecord
    if debug {
        log.Printf("Record %v inserted: %v",silo.last_database_record,  val)
    }
}

func (s *SqlStore) GetRecord(key []byte) record {
        var val []byte
        retval := record{}
        err := s.Dbh().QueryRow("select value from RecordTable where id like ?", key).Scan(&val)

        if val != nil {
            err = json.Unmarshal(val, &retval)
            if err != nil { 
                panic("Could not retrieve record")
            }
        }
        if debug {
            log.Printf("Fetched from database: %v\n", retval)
        }
        return retval
}


func (s *SqlStore) InsertStringAndSymbol(silo *tagSilo, aStr string) {
    silo.count("sql_insert")

    stmt, err := s.Db.Prepare("insert into StringTable(id, value) values(?, ?)")

    if err != nil {
        silo.LogChan["error"] <- fmt.Sprintln("While preparing to insert ", aStr, " into  StringTable: ", err)
    }

    defer stmt.Close()

    _, err = stmt.Exec( silo.next_string_index, []byte(aStr))
    //fmt.Printf("insert into StringTable(id, value) values(%v, %s)\n",silo.next_string_index, aStr)
    if err != nil {
        silo.LogChan["error"] <- fmt.Sprintln("While trying to insert ", aStr, " into StringTable as ", silo.next_string_index, " into ", silo.id, ": ", err)
    }


    silo.count("sql_insert")
    //fmt.Printf("insert into SymbolTable(id, value) values(%s, %v)\n",aStr,  silo.next_string_index)
    stmt, err = s.Db.Prepare("insert into SymbolTable(id, value) values(?, ?)")

    if err != nil {
        silo.LogChan["error"] <- fmt.Sprintln("While preparing to insert  ", aStr, " into SymbolTable: ", err)
    }

    defer stmt.Close()

    _, err = stmt.Exec([]byte(aStr), silo.next_string_index)
    if err != nil {
        silo.LogChan["error"] <- fmt.Sprintln("While trying to insert ", aStr, " into SymbolTable: ", err)
    }
}

func (s *SqlStore) Flush(silo *tagSilo) {
    sqlStmt := `PRAGMA wal_checkpoint(TRUNCATE)`
    _, err := s.Db.Exec(sqlStmt)
		if err != nil {
			silo.LogChan["error"] <- fmt.Sprintf("Performing commit %v", sqlStmt)
		}
}

func (s *SqlStore) GetRecordId(tagID int) []int {
    var retarr []int
    //log.Printf("Fetching %v", tagID)
    rows, err := s.Db.Query("select recordid from TagToRecord where tagid like ?", tagID)
    if err != nil {
    if debug {
        log.Printf("Failed to retrieve tag (%v) because %v", tagID, err)
    }
    return retarr
    }
    defer rows.Close()

    for rows.Next() {
            var res int
            if err := rows.Scan(&res); err != nil {
                    log.Fatal(err)
            }
            retarr = append(retarr, res)

    }
    //log.Printf("Would return %v\n", retarr)
    return retarr
}


func (s *SqlStore) StoreRecordId(key []byte, val []byte)  {
    stmt, err := s.Dbh().Prepare("insert or replace into TagToRecordTable(id, value) values(?, ?)")
    if err != nil {
        panic(fmt.Sprintln("While preparing to insert TagToRecordTable: ", err))
    }

    stmt1, err1 := s.Dbh().Prepare("update TagToRecordTable SET value = ? where id like ? ")
    if err1 != nil {
        panic(fmt.Sprintln("While preparing to update TagToRecordTable: ", err1))
    }
    defer stmt.Close()
    defer stmt1.Close()
    _, err = stmt.Exec(key, val)
    if err != nil {
        panic(fmt.Sprintln("While trying to insert TagToRecordTable: ", err))
    }
    _, err1 = stmt1.Exec(val, key)
    if err1 != nil {
        panic(fmt.Sprintf("Could not store record for key(%v) in TagToRecordTable: %v\n", key, err1))
        return
    }

       //log(fmt.Sprintln("Added record to tag %v (%v)", s.getString(v), v))
}
