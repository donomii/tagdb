package lsmkv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/weaviate/weaviate/adapters/repos/db/lsmkv/segmentindex"
	"github.com/weaviate/weaviate/entities/cyclemanager"
	"github.com/weaviate/weaviate/entities/lsmkv"
	"github.com/weaviate/weaviate/entities/storagestate"
	"github.com/weaviate/weaviate/entities/storobj"
)

type Bucket struct {
	dir      string
	rootDir  string
	active   *Memtable
	flushing *Memtable
	disk     *SegmentGroup
	logger   logrus.FieldLogger

	flushLock sync.RWMutex

	walThreshold      uint64
	flushAfterIdle    time.Duration
	memtableThreshold uint64
	memtableResizer   *memtableSizeAdvisor
	strategy          string
	desiredStrategy   string
	secondaryIndices  uint16

	mmapContents bool

	legacyMapSortingBeforeCompaction bool

	flushCallbackCtrl cyclemanager.CycleCallbackCtrl

	status     storagestate.Status
	statusLock sync.RWMutex

	monitorCount bool

	keepTombstones bool

	useBloomFilter bool

	calcCountNetAdditions bool

	forceCompaction bool
}

func MustNewBucket(ctx context.Context, dir, rootDir string, logger logrus.FieldLogger, compactionCallbacks, flushCallbacks cyclemanager.CycleCallbackGroup, opts ...BucketOption) *Bucket {
	b, err := NewBucket(ctx, dir, rootDir, logger, compactionCallbacks, flushCallbacks, opts...)
	if err != nil {
		panic(err)
	}

	return b
}

func NewBucket(ctx context.Context, dir, rootDir string, logger logrus.FieldLogger, compactionCallbacks, flushCallbacks cyclemanager.CycleCallbackGroup, opts ...BucketOption) (*Bucket, error) {
	defaultMemTableThreshold := uint64(10 * 1024 * 1024)
	defaultWalThreshold := uint64(1024 * 1024 * 1024)
	defaultFlushAfterIdle := 60 * time.Second
	defaultStrategy := StrategyReplace

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	b := &Bucket{
		dir:                   dir,
		rootDir:               rootDir,
		memtableThreshold:     defaultMemTableThreshold,
		walThreshold:          defaultWalThreshold,
		flushAfterIdle:        defaultFlushAfterIdle,
		strategy:              defaultStrategy,
		mmapContents:          true,
		logger:                logger,
		useBloomFilter:        true,
		calcCountNetAdditions: true,
	}

	for _, opt := range opts {
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	if b.memtableResizer != nil {
		b.memtableThreshold = uint64(b.memtableResizer.Initial())
	}

	sg, err := newSegmentGroup(logger, compactionCallbacks,
		sgConfig{
			dir:                   dir,
			strategy:              b.strategy,
			mapRequiresSorting:    b.legacyMapSortingBeforeCompaction,
			monitorCount:          b.monitorCount,
			mmapContents:          b.mmapContents,
			keepTombstones:        b.keepTombstones,
			forceCompaction:       b.forceCompaction,
			useBloomFilter:        b.useBloomFilter,
			calcCountNetAdditions: b.calcCountNetAdditions,
		})
	if err != nil {
		return nil, errors.Wrap(err, "init disk segments")
	}

	if b.strategy == StrategyRoaringSet && len(sg.segments) > 0 &&
		sg.segments[0].strategy == segmentindex.StrategySetCollection {
		b.strategy = StrategySetCollection
		b.desiredStrategy = StrategyRoaringSet
		sg.strategy = StrategySetCollection
	}
	if b.strategy == StrategyRoaringSet && len(sg.segments) > 0 &&
		sg.segments[0].strategy == segmentindex.StrategyMapCollection {
		b.strategy = StrategyMapCollection
		b.desiredStrategy = StrategyRoaringSet
		sg.strategy = StrategyMapCollection
	}

	b.disk = sg

	if err := b.setNewActiveMemtable(); err != nil {
		return nil, err
	}

	if err := b.recoverFromCommitLogs(ctx); err != nil {
		return nil, err
	}

	id := "bucket/flush/" + b.dir
	if flushCallbacks != nil {

		b.flushCallbackCtrl = flushCallbacks.Register(id, b.flushAndSwitchIfThresholdsMet)
	}

	return b, nil
}

func (b *Bucket) Compact() {
	b.disk.Compact()
}

func (b *Bucket) GetDir() string {
	return b.dir
}

func (b *Bucket) GetRootDir() string {
	return b.rootDir
}

func (b *Bucket) GetStrategy() string {
	return b.strategy
}

func (b *Bucket) GetDesiredStrategy() string {
	return b.desiredStrategy
}

func (b *Bucket) GetSecondaryIndices() uint16 {
	return b.secondaryIndices
}

func (b *Bucket) GetStatus() storagestate.Status {
	b.statusLock.RLock()
	defer b.statusLock.RUnlock()

	return b.status
}

func (b *Bucket) GetMemtableThreshold() uint64 {
	return b.memtableThreshold
}

func (b *Bucket) GetWalThreshold() uint64 {
	return b.walThreshold
}

func (b *Bucket) GetFlushAfterIdle() time.Duration {
	return b.flushAfterIdle
}

func (b *Bucket) GetFlushCallbackCtrl() cyclemanager.CycleCallbackCtrl {
	return b.flushCallbackCtrl
}

func (b *Bucket) IterateObjects(ctx context.Context, f func(object *storobj.Object) error) error {
	i := 0
	cursor := b.Cursor()
	defer cursor.Close()

	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		obj, err := storobj.FromBinary(v)
		if err != nil {
			return fmt.Errorf("cannot unmarshal object %d, %v", i, err)
		}
		if err := f(obj); err != nil {
			return errors.Wrapf(err, "callback on object '%d' failed", obj.DocID())
		}

		i++
	}

	return nil
}

func (b *Bucket) IterateMapObjects(ctx context.Context, f func([]byte, []byte, []byte, bool) error) error {
	cursor := b.MapCursor()
	defer cursor.Close()

	for kList, vList := cursor.First(); kList != nil; kList, vList = cursor.Next() {
		for _, v := range vList {
			if err := f(kList, v.Key, v.Value, v.Tombstone); err != nil {
				return errors.Wrapf(err, "callback on object '%v' failed", v)
			}
		}
	}

	return nil
}

func (b *Bucket) SetMemtableThreshold(size uint64) {
	b.memtableThreshold = size
}

func (b *Bucket) Get(key []byte) ([]byte, error) {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	v, err := b.active.get(key)
	if err == nil {
		return v, nil
	}
	if err == lsmkv.Deleted {
		return nil, nil
	}

	if err != lsmkv.NotFound {
		panic(fmt.Sprintf("unsupported error in bucket.Get: %v\n", err))
	}

	if b.flushing != nil {
		v, err := b.flushing.get(key)
		if err == nil {
			return v, nil
		}
		if err == lsmkv.Deleted {
			return nil, nil
		}

		if err != lsmkv.NotFound {
			panic("unsupported error in bucket.Get")
		}
	}

	return b.disk.get(key)
}

func (b *Bucket) GetBySecondary(pos int, key []byte) ([]byte, error) {
	bytes, _, err := b.GetBySecondaryIntoMemory(pos, key, nil)
	return bytes, err
}

func (b *Bucket) GetBySecondaryIntoMemory(pos int, key []byte, buffer []byte) ([]byte, []byte, error) {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	v, err := b.active.getBySecondary(pos, key)
	if err == nil {
		return v, buffer, nil
	}
	if err == lsmkv.Deleted {
		return nil, buffer, nil
	}

	if err != lsmkv.NotFound {
		panic("unsupported error in bucket.Get")
	}

	if b.flushing != nil {
		v, err := b.flushing.getBySecondary(pos, key)
		if err == nil {
			return v, buffer, nil
		}
		if err == lsmkv.Deleted {
			return nil, buffer, nil
		}

		if err != lsmkv.NotFound {
			panic("unsupported error in bucket.Get")
		}
	}

	return b.disk.getBySecondaryIntoMemory(pos, key, buffer)
}

// SetList returns all Set entries for a given key.
//
// SetList is specific to the Set Strategy, for Map use [Bucket.MapList], and
// for Replace use [Bucket.Get].
func (b *Bucket) SetList(key []byte) ([][]byte, error) {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	var out []value

	v, err := b.disk.getCollection(key)
	if err != nil {
		if err != nil && err != lsmkv.NotFound {
			return nil, err
		}
	}
	out = v

	if b.flushing != nil {
		v, err = b.flushing.getCollection(key)
		if err != nil {
			if err != nil && err != lsmkv.NotFound {
				return nil, err
			}
		}
		out = append(out, v...)

	}

	v, err = b.active.getCollection(key)
	if err != nil {
		if err != nil && err != lsmkv.NotFound {
			return nil, err
		}
	}
	if len(v) > 0 {
		// skip the expensive append operation if there was no memtable
		out = append(out, v...)
	}

	return newSetDecoder().Do(out), nil
}

// Put creates or replaces a single value for a given key.
//
//	err := bucket.Put([]byte("my_key"), []byte("my_value"))
//	 if err != nil {
//		/* do something */
//	}
//
// If a bucket has a secondary index configured, you can also specify one or
// more secondary keys, like so:
//
//	err := bucket.Put([]byte("my_key"), []byte("my_value"),
//		WithSecondaryKey(0, []byte("my_alternative_key")),
//	)
//	 if err != nil {
//		/* do something */
//	}
//
// Put is limited to ReplaceStrategy, use [Bucket.SetAdd] for Set or
// [Bucket.MapSet] and [Bucket.MapSetMulti].
func (b *Bucket) Put(key, value []byte, opts ...SecondaryKeyOption) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.put(key, value, opts...)
}

// SetAdd adds one or more Set-Entries to a Set for the given key. SetAdd is
// entirely agnostic of existing entries, it acts as append-only. This also
// makes it agnostic of whether the key already exists or not.
//
// Example to add two entries to a set:
//
//	err := bucket.SetAdd([]byte("my_key"), [][]byte{
//		[]byte("one-set-element"), []byte("another-set-element"),
//	})
//	if err != nil {
//		/* do something */
//	}
//
// SetAdd is specific to the Set strategy. For Replace, use [Bucket.Put], for
// Map use either [Bucket.MapSet] or [Bucket.MapSetMulti].
func (b *Bucket) SetAdd(key []byte, values [][]byte) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.append(key, newSetEncoder().Do(values))
}

// SetDeleteSingle removes one Set element from the given key. Note that LSM
// stores are append only, thus internally this action appends a tombstone. The
// entry will not be removed until a compaction has run, and even then a
// compaction does not guarantee the removal of the data right away. This is
// because an entry could have been created in an older segment than those
// present in the compaction. This can be seen as an implementation detail,
// unless the caller expects to free disk space by calling this method. Such
// freeing is not guaranteed.
//
// SetDeleteSingle is specific to the Set Strategy. For Replace, you can use
// [Bucket.Delete] to delete the entire row, for Maps use [Bucket.MapDeleteKey]
// to delete a single map entry.
func (b *Bucket) SetDeleteSingle(key []byte, valueToDelete []byte) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.append(key, []value{
		{
			value:     valueToDelete,
			tombstone: true,
		},
	})
}

// WasDeleted determines if an object used to exist in the LSM store
//
// There are 3 different locations that we need to check for the key
// in this order: active memtable, flushing memtable, and disk
// segment
func (b *Bucket) WasDeleted(key []byte) (bool, error) {
	if !b.keepTombstones {
		return false, fmt.Errorf("Bucket requires option `keepTombstones` set to check deleted keys")
	}

	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	_, err := b.active.get(key)
	switch err {
	case nil:
		return false, nil
	case lsmkv.Deleted:
		return true, nil
	case lsmkv.NotFound:
		// We can still check flushing and disk
	default:
		return false, fmt.Errorf("unsupported bucket error: %w", err)
	}

	if b.flushing != nil {
		_, err := b.flushing.get(key)
		switch err {
		case nil:
			return false, nil
		case lsmkv.Deleted:
			return true, nil
		case lsmkv.NotFound:
			// We can still check disk
		default:
			return false, fmt.Errorf("unsupported bucket error: %w", err)
		}
	}

	_, err = b.disk.get(key)
	switch err {
	case nil, lsmkv.NotFound:
		return false, nil
	case lsmkv.Deleted:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported bucket error: %w", err)
	}
}

type MapListOptionConfig struct {
	acceptDuplicates           bool
	legacyRequireManualSorting bool
}

type MapListOption func(c *MapListOptionConfig)

func MapListAcceptDuplicates() MapListOption {
	return func(c *MapListOptionConfig) {
		c.acceptDuplicates = true
	}
}

func MapListLegacySortingRequired() MapListOption {
	return func(c *MapListOptionConfig) {
		c.legacyRequireManualSorting = true
	}
}

// MapList returns all map entries for a given row key. The order of map pairs
// has no specific meaning. For efficient merge operations, pair entries are
// stored sorted on disk, however that is an implementation detail and not a
// caller-facing guarantee.
//
// MapList is specific to the Map strategy, for Sets use [Bucket.SetList], for
// Replace use [Bucket.Get].
func (b *Bucket) MapList(key []byte, cfgs ...MapListOption) ([]MapPair, error) {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	c := MapListOptionConfig{}
	for _, cfg := range cfgs {
		cfg(&c)
	}

	segments := [][]MapPair{}
	// before := time.Now()
	disk, err := b.disk.getCollectionBySegments(key)
	if err != nil {
		if err != nil && err != lsmkv.NotFound {
			return nil, err
		}
	}

	for i := range disk {
		segmentDecoded := make([]MapPair, len(disk[i]))
		for j, v := range disk[i] {
			if err := segmentDecoded[j].FromBytes(v.value, false); err != nil {
				return nil, err
			}
			segmentDecoded[j].Tombstone = v.tombstone
		}
		segments = append(segments, segmentDecoded)
	}

	// fmt.Printf("--map-list: get all disk segments took %s\n", time.Since(before))

	// before = time.Now()
	// fmt.Printf("--map-list: append all disk segments took %s\n", time.Since(before))

	if b.flushing != nil {
		v, err := b.flushing.getMap(key)
		if err != nil {
			if err != nil && err != lsmkv.NotFound {
				return nil, err
			}
		}

		segments = append(segments, v)
	}

	// before = time.Now()
	v, err := b.active.getMap(key)
	if err != nil {
		if err != nil && err != lsmkv.NotFound {
			return nil, err
		}
	}
	segments = append(segments, v)
	// fmt.Printf("--map-list: get all active segments took %s\n", time.Since(before))

	// before = time.Now()
	// defer func() {
	// 	fmt.Printf("--map-list: run decoder took %s\n", time.Since(before))
	// }()

	if c.legacyRequireManualSorting {
		// Sort to support segments which were stored in an unsorted fashion
		for i := range segments {
			sort.Slice(segments[i], func(a, b int) bool {
				return bytes.Compare(segments[i][a].Key, segments[i][b].Key) == -1
			})
		}
	}

	return newSortedMapMerger().do(segments)
}

// MapSet writes one [MapPair] into the map for the given row key. It is
// agnostic of whether the row key already exists, as well as agnostic of
// whether the map key already exists. In both cases it will create the entry
// if it does not exist or override if it does.
//
// Example to add a new MapPair:
//
//	pair := MapPair{Key: []byte("Jane"), Value: []byte("Backend")}
//	err := bucket.MapSet([]byte("developers"), pair)
//	if err != nil {
//		/* do something */
//	}
//
// MapSet is specific to the Map Strategy, for Replace use [Bucket.Put], and for Set use [Bucket.SetAdd] instead.
func (b *Bucket) MapSet(rowKey []byte, kv MapPair) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.appendMapSorted(rowKey, kv)
}

// MapSetMulti is the same as [Bucket.MapSet], except that it takes in multiple
// [MapPair] objects at the same time.
func (b *Bucket) MapSetMulti(rowKey []byte, kvs []MapPair) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	for _, kv := range kvs {
		if err := b.active.appendMapSorted(rowKey, kv); err != nil {
			return err
		}
	}

	return nil
}

// MapDeleteKey removes one key-value pair from the given map row. Note that
// LSM stores are append only, thus internally this action appends a tombstone.
// The entry will not be removed until a compaction has run, and even then a
// compaction does not guarantee the removal of the data right away. This is
// because an entry could have been created in an older segment than those
// present in the compaction. This can be seen as an implementation detail,
// unless the caller expects to free disk space by calling this method. Such
// freeing is not guaranteed.
//
// MapDeleteKey is specific to the Map Strategy. For Replace, you can use
// [Bucket.Delete] to delete the entire row, for Sets use [Bucket.SetDeleteSingle] to delete a single set element.
func (b *Bucket) MapDeleteKey(rowKey, mapKey []byte) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	pair := MapPair{
		Key:       mapKey,
		Tombstone: true,
	}

	return b.active.appendMapSorted(rowKey, pair)
}

// Delete removes the given row. Note that LSM stores are append only, thus
// internally this action appends a tombstone.  The entry will not be removed
// until a compaction has run, and even then a compaction does not guarantee
// the removal of the data right away. This is because an entry could have been
// created in an older segment than those present in the compaction. This can
// be seen as an implementation detail, unless the caller expects to free disk
// space by calling this method. Such freeing is not guaranteed.
//
// Delete is specific to the Replace Strategy. For Maps, you can use
// [Bucket.MapDeleteKey] to delete a single key-value pair, for Sets use
// [Bucket.SetDeleteSingle] to delete a single set element.
func (b *Bucket) Delete(key []byte, opts ...SecondaryKeyOption) error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.setTombstone(key, opts...)
}

// meant to be called from situations where a lock is already held, does not
// lock on its own
func (b *Bucket) setNewActiveMemtable() error {
	mt, err := newMemtable(filepath.Join(b.dir, fmt.Sprintf("segment-%d",
		time.Now().UnixNano())), b.strategy, b.secondaryIndices)
	if err != nil {
		return err
	}

	b.active = mt
	return nil
}

func (b *Bucket) Count() int {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	if b.strategy != StrategyReplace {
		panic("Count() called on strategy other than 'replace'")
	}

	memtableCount := 0
	if b.flushing == nil {
		// only consider active
		memtableCount += b.memtableNetCount(b.active.countStats(), nil)
	} else {
		flushingCountStats := b.flushing.countStats()
		activeCountStats := b.active.countStats()
		deltaActive := b.memtableNetCount(activeCountStats, flushingCountStats)
		deltaFlushing := b.memtableNetCount(flushingCountStats, nil)

		memtableCount = deltaActive + deltaFlushing
	}

	diskCount := b.disk.count()

	return memtableCount + diskCount
}

func (b *Bucket) memtableNetCount(stats *countStats, previousMemtable *countStats) int {
	netCount := 0

	// TODO: this uses regular get, given that this may be called quite commonly,
	// we might consider building a pure Exists(), which skips reading the value
	// and only checks for tombstones, etc.
	for _, key := range stats.upsertKeys {
		if !b.existsOnDiskAndPreviousMemtable(previousMemtable, key) {
			netCount++
		}
	}

	for _, key := range stats.tombstonedKeys {
		if b.existsOnDiskAndPreviousMemtable(previousMemtable, key) {
			netCount--
		}
	}

	return netCount
}

func (b *Bucket) existsOnDiskAndPreviousMemtable(previous *countStats, key []byte) bool {
	v, _ := b.disk.get(key) // current implementation can't error
	if v == nil {
		// not on disk, but it could still be in the previous memtable
		return previous.hasUpsert(key)
	}

	// it exists on disk ,but it could still have been deleted in the previous memtable
	return !previous.hasTombstone(key)
}

func (b *Bucket) Shutdown(ctx context.Context) error {
	if err := b.disk.shutdown(ctx); err != nil {
		return err
	}

	if err := b.flushCallbackCtrl.Unregister(ctx); err != nil {
		return errors.Wrap(ctx.Err(), "long-running flush in progress")
	}

	b.flushLock.Lock()
	if err := b.active.flush(); err != nil {
		return err
	}
	b.flushLock.Unlock()

	if b.flushing == nil {
		// active has flushing, no one else was currently flushing, it's safe to
		// exit
		return nil
	}

	// it seems we still need to wait for someone to finish flushing
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if b.flushing == nil {
				return nil
			}
		}
	}
}

func (b *Bucket) flushAndSwitchIfThresholdsMet(shouldAbort cyclemanager.ShouldAbortCallback) bool {
	b.flushLock.RLock()
	commitLogSize := b.active.commitlog.Size()
	memtableTooLarge := b.active.Size() >= b.memtableThreshold
	walTooLarge := uint64(commitLogSize) >= b.walThreshold
	dirtyButIdle := (b.active.Size() > 0 || commitLogSize > 0) &&
		b.active.IdleDuration() >= b.flushAfterIdle
	shouldSwitch := memtableTooLarge || walTooLarge || dirtyButIdle

	// If true, the parent shard has indicated that it has
	// entered an immutable state. During this time, the
	// bucket should refrain from flushing until its shard
	// indicates otherwise
	if shouldSwitch && b.isReadOnly() {
		b.logger.WithField("action", "lsm_memtable_flush").
			WithField("path", b.dir).
			Warn("flush halted due to shard READONLY status")

		b.flushLock.RUnlock()
		// TODO maybe will not be necessary with dynamic interval
		time.Sleep(time.Second)
		return false
	}

	b.flushLock.RUnlock()
	if shouldSwitch {
		cycleLength := b.active.ActiveDuration()
		if err := b.FlushAndSwitch(); err != nil {
			b.logger.WithField("action", "lsm_memtable_flush").
				WithField("path", b.dir).
				WithError(err).
				Errorf("flush and switch failed")
		}

		if b.memtableResizer != nil {
			next, ok := b.memtableResizer.NextTarget(int(b.memtableThreshold), cycleLength)
			if ok {
				b.memtableThreshold = uint64(next)
			}
		}
		return true
	}
	return false
}

// UpdateStatus is used by the parent shard to communicate to the bucket
// when the shard has been set to readonly, or when it is ready for
// writes.
func (b *Bucket) UpdateStatus(status storagestate.Status) {
	b.statusLock.Lock()
	defer b.statusLock.Unlock()

	b.status = status
	b.disk.updateStatus(status)
}

func (b *Bucket) isReadOnly() bool {
	b.statusLock.Lock()
	defer b.statusLock.Unlock()

	return b.status == storagestate.StatusReadOnly
}

// FlushAndSwitch is typically called periodically and does not require manual
// calling, but there are some situations where this might be intended, such as
// in test scenarios or when a force flush is desired.
func (b *Bucket) FlushAndSwitch() error {
	before := time.Now()

	b.logger.WithField("action", "lsm_memtable_flush_start").
		WithField("path", b.dir).
		Trace("start flush and switch")
	if err := b.atomicallySwitchMemtable(); err != nil {
		return errors.Wrap(err, "switch active memtable")
	}

	if err := b.flushing.flush(); err != nil {
		return errors.Wrap(err, "flush")
	}

	if err := b.atomicallyAddDiskSegmentAndRemoveFlushing(); err != nil {
		return errors.Wrap(err, "add segment and remove flushing")
	}

	took := time.Since(before)
	b.logger.WithField("action", "lsm_memtable_flush_complete").
		WithField("path", b.dir).
		Trace("finish flush and switch")

	b.logger.WithField("action", "lsm_memtable_flush_complete").
		WithField("path", b.dir).
		WithField("took", took).
		Debugf("flush and switch took %s\n", took)

	return nil
}

func (b *Bucket) atomicallyAddDiskSegmentAndRemoveFlushing() error {
	b.flushLock.Lock()
	defer b.flushLock.Unlock()

	path := b.flushing.path
	if err := b.disk.add(path + ".db"); err != nil {
		return err
	}
	b.flushing = nil

	return nil
}

func (b *Bucket) atomicallySwitchMemtable() error {
	b.flushLock.Lock()
	defer b.flushLock.Unlock()

	b.flushing = b.active
	return b.setNewActiveMemtable()
}

func (b *Bucket) Strategy() string {
	return b.strategy
}

func (b *Bucket) DesiredStrategy() string {
	return b.desiredStrategy
}

// the WAL uses a buffer and isn't written until the buffer size is crossed or
// this function explicitly called. This allows to avoid unnecessary disk
// writes in larger operations, such as batches. It is sufficient to call write
// on the WAL just once. This does not make a batch atomic, but it guarantees
// that the WAL is written before a successful response is returned to the
// user.
func (b *Bucket) WriteWAL() error {
	b.flushLock.RLock()
	defer b.flushLock.RUnlock()

	return b.active.writeWAL()
}