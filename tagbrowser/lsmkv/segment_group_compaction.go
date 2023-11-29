//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \ V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2023 Weaviate B.V. All rights reserved.
//
//  CONTACT: hello@weaviate.io
//

package lsmkv

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/weaviate/weaviate/adapters/repos/db/lsmkv/roaringset"
	"github.com/weaviate/weaviate/adapters/repos/db/lsmkv/segmentindex"
	"github.com/weaviate/weaviate/entities/cyclemanager"
)

func (sg *SegmentGroup) bestCompactionCandidatePair() []int {
	sg.maintenanceLock.RLock()
	defer sg.maintenanceLock.RUnlock()

	if sg.isReadyOnly() {
		return nil
	}

	if len(sg.segments) < 2 {
		return nil
	}

	levels := map[uint16]int{}
	lowestPairLevel := uint16(math.MaxUint16)
	lowestLevel := uint16(math.MaxUint16)
	lowestIndex := -1
	secondLowestIndex := -1
	pairExists := false

	for ind, seg := range sg.segments {
		levels[seg.level]++
		val := levels[seg.level]
		if val > 1 {
			if seg.level < lowestPairLevel {
				lowestPairLevel = seg.level
				pairExists = true
			}
		}

		if seg.level < lowestLevel {
			secondLowestIndex = lowestIndex
			lowestLevel = seg.level
			lowestIndex = ind
		}
	}

	if pairExists {
		var res []int

		for i, segment := range sg.segments {
			if len(res) >= 2 {
				break
			}

			if segment.level == lowestPairLevel {
				res = append(res, i)
			}
		}

		return res
	} else {
		if sg.compactLeftOverSegments {
			return []int{secondLowestIndex, lowestIndex}
		} else {
			return nil
		}
	}
}

func (sg *SegmentGroup) segmentAtPos(pos int) *segment {
	sg.maintenanceLock.RLock()
	defer sg.maintenanceLock.RUnlock()

	return sg.segments[pos]
}

func (sg *SegmentGroup) compactOnce() (bool, error) {
	pair := sg.bestCompactionCandidatePair()
	if pair == nil {
		return false, nil
	}

	path := fmt.Sprintf("%s.tmp", sg.segmentAtPos(pair[1]).path)
	f, err := os.Create(path)
	if err != nil {
		return false, err
	}

	scratchSpacePath := sg.segmentAtPos(pair[1]).path + "compaction.scratch.d"

	level := sg.segmentAtPos(pair[0]).level
	secondaryIndices := sg.segmentAtPos(pair[0]).secondaryIndexCount

	if level == sg.segmentAtPos(pair[1]).level {
		level = level + 1
	}

	strategy := sg.segmentAtPos(pair[0]).strategy
	cleanupTombstones := !sg.keepTombstones && pair[0] == 0

	switch strategy {
	case segmentindex.StrategyReplace:
		c := newCompactorReplace(f, sg.segmentAtPos(pair[0]).newCursor(),
			sg.segmentAtPos(pair[1]).newCursor(), level, secondaryIndices, scratchSpacePath, cleanupTombstones)

		if err := c.do(); err != nil {
			return false, err
		}
	case segmentindex.StrategySetCollection:
		c := newCompactorSetCollection(f, sg.segmentAtPos(pair[0]).newCollectionCursor(),
			sg.segmentAtPos(pair[1]).newCollectionCursor(), level, secondaryIndices,
			scratchSpacePath, cleanupTombstones)

		if err := c.do(); err != nil {
			return false, err
		}
	case segmentindex.StrategyMapCollection:
		c := newCompactorMapCollection(f,
			sg.segmentAtPos(pair[0]).newCollectionCursorReusable(),
			sg.segmentAtPos(pair[1]).newCollectionCursorReusable(),
			level, secondaryIndices, scratchSpacePath, sg.mapRequiresSorting, cleanupTombstones)

		if err := c.do(); err != nil {
			return false, err
		}
	case segmentindex.StrategyRoaringSet:
		leftSegment := sg.segmentAtPos(pair[0])
		rightSegment := sg.segmentAtPos(pair[1])

		leftCursor := leftSegment.newRoaringSetCursor()
		rightCursor := rightSegment.newRoaringSetCursor()

		c := roaringset.NewCompactor(f, leftCursor, rightCursor,
			level, scratchSpacePath, cleanupTombstones)

		if err := c.Do(); err != nil {
			return false, err
		}

	default:
		return false, fmt.Errorf("unrecognized strategy %v", strategy)
	}

	if err := f.Close(); err != nil {
		return false, fmt.Errorf("close compacted segment file: %w", err)
	}

	if err := sg.replaceCompactedSegments(pair[0], pair[1], path); err != nil {
		return false, fmt.Errorf("replace compacted segments: %w", err)
	}

	return true, nil
}

func (sg *SegmentGroup) replaceCompactedSegments(old1, old2 int, newPathTmp string) error {
	sg.maintenanceLock.RLock()
	updatedCountNetAdditions := sg.segments[old1].countNetAdditions +
		sg.segments[old2].countNetAdditions
	sg.maintenanceLock.RUnlock()

	precomputedFiles, err := preComputeSegmentMeta(newPathTmp,
		updatedCountNetAdditions, sg.logger,
		sg.useBloomFilter, sg.calcCountNetAdditions)
	if err != nil {
		return fmt.Errorf("precompute segment meta: %w", err)
	}

	sg.maintenanceLock.Lock()
	defer sg.maintenanceLock.Unlock()

	if err := sg.segments[old1].close(); err != nil {
		return fmt.Errorf("close disk segment: %w", err)
	}

	if err := sg.segments[old2].close(); err != nil {
		return fmt.Errorf("close disk segment: %w", err)
	}

	if err := sg.segments[old1].drop(); err != nil {
		return fmt.Errorf("drop disk segment: %w", err)
	}

	if err := sg.segments[old2].drop(); err != nil {
		return fmt.Errorf("drop disk segment: %w", err)
	}

	sg.segments[old1] = nil
	sg.segments[old2] = nil

	var newPath string
	for i, path := range precomputedFiles {
		updated, err := sg.stripTmpExtension(path)
		if err != nil {
			return fmt.Errorf("strip .tmp extension of new segment: %w", err)
		}

		if i == 0 {
			newPath = updated
		}
	}

	seg, err := newSegment(newPath, sg.logger, nil,
		sg.mmapContents, sg.useBloomFilter, sg.calcCountNetAdditions)
	if err != nil {
		return fmt.Errorf("create new segment: %w", err)
	}

	sg.segments[old2] = seg

	sg.segments = append(sg.segments[:old1], sg.segments[old1+1:]...)

	return nil
}

func (sg *SegmentGroup) stripTmpExtension(oldPath string) (string, error) {
	ext := filepath.Ext(oldPath)
	if ext != ".tmp" {
		return "", fmt.Errorf("segment %q did not have .tmp extension", oldPath)
	}
	newPath := oldPath[:len(oldPath)-len(ext)]

	if err := os.Rename(oldPath, newPath); err != nil {
		return "", fmt.Errorf("rename %q -> %q: %w", oldPath, newPath, err)
	}

	return newPath, nil
}

func (sg *SegmentGroup) compactIfLevelsMatch(shouldAbort cyclemanager.ShouldAbortCallback) bool {

	compacted, err := sg.compactOnce()
	if err != nil {
		sg.logger.WithField("action", "lsm_compaction").
			WithField("path", sg.dir).
			WithError(err).
			Errorf("compaction failed")
	}

	if compacted {
		return true
	} else {
		sg.logger.WithField("action", "lsm_compaction").
			WithField("path", sg.dir).
			Trace("no segment eligible for compaction")
		return false
	}
}

func (sg *SegmentGroup) Len() int {
	sg.maintenanceLock.RLock()
	defer sg.maintenanceLock.RUnlock()

	return len(sg.segments)
}
