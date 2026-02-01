# Tagdb Specification

## Overview

**Tagdb** is a text search engine built around a pure inverted index. It indexes files, webpages, and other content, storing them as "records" with associated "tags" (words/tokens). It's designed for fast word completion and real-time search. This project was created as a learning exercise for Go and databases.

---

## Architecture

### Components

The system is organized into several independent command-line binaries, all built around a shared core library (`tagbrowser`).

```mermaid
graph TD
    subgraph Client Layer
        tagloader["tagloader (Indexer CLI)"]
        tagquery["tagquery (Query CLI)"]
        tagshell["tagshell (Interactive TUI)"]
        fetchbot["fetchbot (Web Crawler)"]
    end

    subgraph Server Layer
        tagserver["tagserver (Main Server)"]
    end

    tagloader -->|JSON-RPC| tagserver
    tagquery -->|JSON-RPC| tagserver
    tagshell -->|JSON-RPC| tagserver
    fetchbot -->|JSON-RPC| tagserver

    subgraph tagbrowser (Core Library)
        rpc["RPC Server"]
        manor["Manor"]
        farm1["Farm"]
        farm2["Farm"]
        silo1["tagSilo"]
        silo2["tagSilo"]
        silo3["tagSilo"]
    end
    
    tagserver --> rpc
    rpc --> manor
    manor --> farm1
    manor --> farm2
    farm1 --> silo1
    farm1 --> silo2
    farm2 --> silo3
```

| Binary | Description |
|--------|-------------|
| `tagserver` | The main database server. Listens for JSON-RPC requests on TCP port `6781`. Handles `SIGINT`/`SIGTERM` for graceful shutdown. |
| `tagloader` | Crawls directories and indexes files. Provides verbose logging of progress. |
| `tagquery` | Sends search queries or admin commands (shutdown, status) to the server. |
| `tagshell` | An interactive terminal UI (using `termbox-go`) for searching. |
| `fetchbot` | A web crawler (using `puerkitobio/fetchbot`) that indexes web pages. |

### Core Library (`tagbrowser` package)

The hierarchy of data management objects is:
- **`Manor`**: The top-level manager. It owns multiple `Farm`s and a central channel for accepting incoming records. Queries are fanned out to all farms.
- **`Farm`**: Manages a collection of `tagSilo`s in a single directory. Handles sharding and parallelizes searches across its silos.
- **`tagSilo`**: The atomic unit of storage. Contains the inverted index, string interning tables, and delegates to a `SiloStore` for persistence.
  - *Refactoring Note*: `tagSilo` implementation is split across `silo.go` (core), `silo_workers.go` (background tasks), `silo_records.go` (CRUD), `silo_search.go` (search logic), and `silo_symbols.go` (string interning).

---

## Data Structures

### `tagSilo`
The core data structure.

| Field | Type | Description |
|-------|------|-------------|
| `database` | `[]record` | In-memory record store (for memory-only silos). |
| `string_table` | `*patricia.Trie` | Maps tag strings to integer symbols. |
| `reverse_string_table` | `[]string` | Maps integer symbols back to strings. |
| `tag2file` | `[][]*record` | The inverted index: `tag2file[tagID]` is a list of records with that tag. |
| `Store` | `SiloStore` | Pluggable persistence backend. |

### `record`
Represents a single indexed item (e.g., a line in a file).

```go
type record struct {
    Filename    int           // Symbol ID of the filename
    Line        int           // Line number in the file
    Fingerprint fingerPrint   // []int - List of tag symbol IDs in this record
}
```

---

## Storage Layer

The persistence layer is abstracted behind the `SiloStore` interface:

```go
type SiloStore interface {
    Init(silo *tagSilo)
    GetString(s *tagSilo, index int) string
    GetSymbol(silo *tagSilo, aStr string) int
    InsertRecord(silo *tagSilo, key []byte, aRecord record)
    InsertStringAndSymbol(silo *tagSilo, aStr string)
    Flush(silo *tagSilo)
    GetRecordId(tagID int) []int
    StoreRecordId(key, val []byte)
    GetRecord(key []byte) record
    StoreTagToRecord(recordId int, fp fingerPrint)
}
```

| Implementation | Description |
|----------------|-------------|
| `SqlStore` (in `sqlsilo.go`) | Uses SQLite via `mattn/go-sqlite3`. |
| `WeaviateStore` (in `silolsm.go`) | Uses the vendored `lsmkv` library (an LSM-tree implementation originally from Weaviate). |

---

## API (JSON-RPC)

The server exposes the following RPC methods on the `TagResponder` service:

| Method | Args | Reply | Description |
|--------|------|-------|-------------|
| `SearchString` | `Args{A: string, Limit: int}` | `Reply{C: []ResultRecordTransmittable}` | Performs a multi-farm search. |
| `PredictString` | `Args` | `StringListReply` | (Currently incomplete) Word completion. |
| `InsertRecord` | `InsertArgs{Name, Position, Tags}` | `SuccessReply` | Adds a new record to the index. |
| `Status` | `Args` | `StatusReply` | Returns server statistics (currently sparse). |
| `Shutdown` | `Args` | `SuccessReply` | Gracefully shuts down the server. |

---

## Configuration

The server is configured via a TOML file (`tagdb.conf`).

```toml
[database]
    Server = "127.0.0.1"

[Farms.a]
    Location = "./database/partition1"
    Silos    = 1
    Mode     = "disk"  # "memory" or "disk"
    Offload  = false
    Size     = 1000000
```

---

## What's Worth Salvaging

| Component | Verdict | Notes |
|-----------|---------|-------|
| **Core Inverted Index Logic** (`silo.go`) | ✅ Keep | The fundamental search algorithms (`scanFileDatabase`, `score`, `makeFingerprint`) are sound. The logic for scoring results by number of matching tags is useful. |
| **`SiloStore` Interface** | ✅ Keep | A good abstraction. Allows swapping storage backends. |
| **`WeaviateStore` / `lsmkv`** | ⚠️ Evaluate | The vendored `lsmkv` library is substantial (~150 files, extensive tests). It's a serious LSM-tree implementation. However, it's a fork. Consider updating to the upstream or using a more standard KV store (e.g., BadgerDB, Pebble). |
| **`SqlStore`** | ✅ Keep | SQLite is a simple, proven backend. Useful for small-scale use. |
| **JSON-RPC Server** | ⚠️ Refactor | Works, but `net/rpc` is old. Consider gRPC or a REST API for better tooling. The current `jsonrpc` approach is dated. |
| **`tagshell`** | ⚠️ Evaluate | Depends on `termbox-go`, which has known issues with modern terminals. Consider `bubbletea` or `fyne` for a modern TUI. |
| **Concurrency Model** | ✅ Improved | Switched to `genericsyncmap` for concurrent map access. Removed spin loops in favor of `sync.WaitGroup`. Fine-grained locking with `defer` ensures safety. |
| **Global Variables** | ❌ Remove | `debug`, `defaultManor`, `rpcClient`, `ServerAddress` are global. Inject them via functions or structs. |

---

## Improvement Plan

### Phase 1: Modernization

1.  **Go Modules**: Initialize `go.mod`. The project currently uses relative imports (e.g., `"../../tagbrowser"`), which are non-standard.
2.  **Dependency Audit**: Review and update dependencies. Remove unused imports in `main.go`.
3.  **Fix Relative Imports**: The `lsmkv` package is imported as `"./lsmkv"`. Move it to a proper module path or use a `replace` directive.

### Phase 2: Code Cleanup

1.  **Remove Dead Code**: The codebase has many FIXME comments and commented-out code blocks. Clean these up.
2.  **Error Handling**: Replace `panic()` calls with proper error returns. Functions like `getDiskRecord` should not panic.
3.  **Logging Channels**: The `LogChan` system is complex. Replace with a structured logger like `zap` or `slog`.
4.  **Eliminate Global State**: Inject `defaultManor` and configuration into handlers.

### Phase 3: Concurrency

1.  **Replace Spin Loops**: The `pending` counter pattern with `time.Sleep` should be replaced with `sync.WaitGroup`.
    ```go
    // Before:
    for i := 0; pending > 0; i = i + 1 {
        time.Sleep(1.0 * time.Millisecond)
    }
    // After:
    var wg sync.WaitGroup
    wg.Wait()
    ```
2.  **Address FIXME Race Conditions**: Review comments like `//FIXME race condition against shutdown`.

### Phase 4: API

1.  **Consider gRPC**: For a structured, typed API. Or a REST API with OpenAPI spec for broader tooling.
2.  **Implement `PredictString`**: The RPC method exists but is incomplete.

### Phase 5: Storage

1.  **Evaluate `lsmkv` fork**: The vendored library is large. If you don't need its specific features, consider BadgerDB or Pebble. If you do, consider tracking the upstream Weaviate project.

---

## Summary

The tagdb project has a solid core idea: a sharded, pluggable inverted index for text search. The `SiloStore` abstraction and the basic search logic are good. However, the codebase shows its age. The main areas for improvement are:

1.  **Modernize the build system** (Go modules).
2.  **Clean up error handling and logging**.
3.  **Fix concurrency patterns** (replace spin loops with proper sync primitives).
4.  **Decide on the storage backend** (evaluate the vendored `lsmkv` vs. alternatives).
5.  **Consider a modern API** (gRPC or REST).

The project is a reasonable foundation for a personal search tool or a learning project. With refactoring, it could be a usable library.
