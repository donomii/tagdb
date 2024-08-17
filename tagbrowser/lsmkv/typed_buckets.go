// LSMKV is weaviate's basic data store.
package lsmkv

import (
	"context"
	"fmt"

	"io/ioutil"

	"github.com/sirupsen/logrus"
	"github.com/weaviate/sroar"
	"github.com/weaviate/weaviate/entities/storobj"
)

// NewNullLogger creates a discarding logger and installs the test hook.
func NewNullLogger() *logrus.Logger {

	logger := logrus.New()
	logger.Out = ioutil.Discard

	return logger

}


// ReplaceableBucket is the "traditional" kv store.  You can set and get key-value pairs.
type ReplaceableBucket struct {
	weaviateBucket *Bucket
}


func NewReplaceableBucket (dir string) *ReplaceableBucket {
	n := NewNullLogger()
	return &ReplaceableBucket{MustNewBucket( context.Background(), dir, "", n, nil, nil,WithStrategy(StrategyReplace))}
}

// Lsmkv creates temporary files to rapidly store incoming data.  Compact merges the files, which makes them quicker and more efficient to query.  This must be called explicitly, it does not run in the background, automatically.  It is thread safe, you can start your own background thread to call it.
func (b *ReplaceableBucket) Compact() {
	b.weaviateBucket.Compact()
}

// Get a value from the store.
func (b *ReplaceableBucket) Get(key []byte) ([]byte, error) {
	return b.weaviateBucket.Get(key)
}

// Set a value in the store.  If the key already exists, it will be overwritten.
func (b *ReplaceableBucket) Put(key []byte, value []byte) error {
	return b.weaviateBucket.Put(key, value)
}


// Set a value in the store.  If the key already exists, it will be overwritten.
func (b *ReplaceableBucket) Set(key []byte, value []byte) error {
	return b.weaviateBucket.Put(key, value)
}

//Delete a key from the store.
func (b *ReplaceableBucket) Delete(key []byte) error {
	return b.weaviateBucket.Delete(key)
}

// Check if a key exists in the store.
func (b *ReplaceableBucket) Exists(key []byte) bool {
	_,err := b.Get(key)
	return err==nil
}

// Count the number of keys in the store.
func (b *ReplaceableBucket) Count() int {
	return b.weaviateBucket.Count()
}

// Iterate over all the keys in the store.
func (b *ReplaceableBucket) Range(f func(object *storobj.Object) error) error {
	return b.weaviateBucket.IterateObjects(context.Background(), f)
}

// Shutdown the store.  You should call this before exiting your program, to flush caches and memtables.
func (b *ReplaceableBucket) Shutdown(ctx context.Context) error {
	return b.weaviateBucket.Shutdown(ctx)
}

// RoaringBucket is a kv store that uses roaring bitmaps to store sets of uint64.  You can add and remove values from the set, and check if a value is in the set.  Roaring bitmaps are fast and memory efficient.
type RoaringBucket struct {
	weaviateBucket *Bucket
}

// Create a new roaring set bucket.
func NewRoaringBucket (dir string) *RoaringBucket {
	n := NewNullLogger()
	return &RoaringBucket{MustNewBucket( context.Background(), dir ,"", n, nil, nil,nil,WithStrategy(StrategyRoaringSet))}
}

// Lsmkv creates temporary files to rapidly store incoming data.  Compact merges the files, which makes them quicker and more efficient to query.  This must be called explicitly, it does not run in the background, automatically.  It is thread safe, you can start your own background thread to call it.
func (b *RoaringBucket) Compact() {
	b.weaviateBucket.Compact()
}

// Get a roaring bitmap from the store.  You will then have to work with the roaring bitmap to get the values.
func (b *RoaringBucket) Get(key []byte) (*sroar.Bitmap, error) {
	return b.weaviateBucket.RoaringSetGet(key)
}

//TODO make function existsInSet?

// Set a roaring bitmap in the store.  If the key already exists, it will be overwritten.
func (b *RoaringBucket) Put(key []byte, bm *sroar.Bitmap) error {
	return b.weaviateBucket.RoaringSetAddBitmap(key, bm)
}

// Set a roaring bitmap in the store.  If the key already exists, it will be overwritten.
func (b *RoaringBucket) Set(key []byte, bm *sroar.Bitmap) error {
	return b.weaviateBucket.RoaringSetAddBitmap(key, bm)
}

// Add a value to a roaring bitmap in the store.  If the set does not already exist, it will be created.
func (b *RoaringBucket) AddToSet(key []byte, value uint64) error {
	return b.weaviateBucket.RoaringSetAddOne(key, value)
}

// Remove a value from a roaring bitmap in the store.
func (b *RoaringBucket) RemoveFromSet(key []byte, value uint64) error {
	return b.weaviateBucket.RoaringSetRemoveOne(key, value)
}

// Add a list of values to a roaring bitmap in the store.  If the set does not already exist, it will be created.
func (b *RoaringBucket) AddListToSet(key []byte, values []uint64) error {
	return b.weaviateBucket.RoaringSetAddList(key, values)
}


func (b *RoaringBucket) Delete(key []byte) error {
	return b.weaviateBucket.Delete(key)
}

func (b *RoaringBucket) Exists(key []byte) bool {
	_,err := b.Get(key)
	return err==nil
}

func (b *RoaringBucket) Cursor() CursorRoaringSet {
	return b.weaviateBucket.CursorRoaringSet()
}

func (b *RoaringBucket) Range(f func(key []byte, bm *sroar.Bitmap) error) error {
	i := 0
	cursor := b.Cursor()
	defer cursor.Close()

	for k, bm := cursor.First(); k != nil; k, bm = cursor.Next() {
		if err := f(k,bm); err != nil {
			return fmt.Errorf("error in range function for key: %v, %v", k, err)
		}

		i++
	}

	return nil
}

func (b *RoaringBucket) Count() int {
	return b.weaviateBucket.Count()
}

func (b *RoaringBucket) Shutdown(ctx context.Context) error {
	b.weaviateBucket.Shutdown(ctx)
	return nil
}




