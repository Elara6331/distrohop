/*
 * distrohop - A utility for correlating and identifying equivalent software
 * packages across different Linux distributions
 *
 * Copyright (C) 2025 Elara Ivy <elara@elara.ws>
 *
 * This file is part of distrohop.
 *
 * distrohop is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * distrohop is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with distrohop.  If not, see <http://www.gnu.org/licenses/>.
 */

package store

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/cockroachdb/pebble"
	"github.com/zeebo/sbloom"
	"go.elara.ws/distrohop/internal/index"
)

// ErrBlocked is returned when a store's database is being updated
var ErrBlocked = errors.New("database is being updated; please try again later")

func init() {
	gob.Register(&xxhash.Digest{})
}

// Package represents a software package with a name and associated tags
type Package struct {
	// The name of the package
	Name string
	// A list of tags associated with the package
	Tags []string
}

type nopLogger struct{}

func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Fatalf(string, ...any) {}

// ReadOnly represents a read-only package store
type ReadOnly interface {
	GetPkg(name string) (Package, error)
	GetPkgNamesByPrefix(prefix string, n int) ([]string, error)
	Search(tags []string) ([]TagResult, time.Duration, error)
}

// Store represents persistent storage for package data
type Store struct {
	Path string
	db   *pebble.DB

	// blocked is used to ensure that all other operations finish
	// running before a [Store.Replace] operation, and cannot
	// start running until the replace operation is completed.
	// [Store.Replace] does a write lock on the RWMutex. All
	// other operations do read locks.
	blocked sync.RWMutex

	// SearchThreads is the number of worker goroutines to be used
	// for searching the database for a tag. The default is 4.
	SearchThreads int
}

// Open initializes and opens a [Store] at the specified path
func Open(path string) (*Store, error) {
	db, err := pebble.Open(path, &pebble.Options{Logger: nopLogger{}})
	if err != nil {
		return nil, err
	}
	return &Store{
		Path:          path,
		db:            db,
		SearchThreads: 4,
	}, err
}

// WriteBatch writes a batch of index records to the store.
// It merges existing tags with new ones and ensures they're unique.
func (s *Store) WriteBatch(batch map[string]index.Record, filters map[byte]*sbloom.Filter) error {
	if !s.blocked.TryRLock() {
		return ErrBlocked
	}
	defer s.blocked.RUnlock()

	b := s.db.NewBatch()
	defer b.Close()

	for _, item := range batch {
		if len(item.Name) == 0 || len(item.Tags) == 0 {
			continue
		}

		key := unsafeBytes(item.Name)

		curVal, cl, err := s.db.Get(key)
		if err == pebble.ErrNotFound {
			// Remove any duplicate tags
			slices.Sort(item.Tags)
			tags := slices.Compact(item.Tags)
			// Write the new package to the database
			err := b.Set(key, joinTags(item.Name[0], tags, filters), nil)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			// Since the package already exists in the database, combine its existing
			// tags with the ones we just got
			tags := strings.Split(unsafeString(curVal), "\x1F")
			tags = append(tags, item.Tags...)
			// Remove any duplicate tags
			slices.Sort(tags)
			tags = slices.Compact(tags)
			// Write the updated package to the database
			err := b.Set(key, joinTags(item.Name[0], tags, filters), nil)
			if err != nil {
				cl.Close()
				return err
			}
			cl.Close()
		}
	}
	// Commit the batch to persistent storage
	return b.Commit(nil)
}

// WriteFilters writes bloom filters for each package name starting character
// to the database.
func (s *Store) WriteFilters(filters map[byte]*sbloom.Filter) error {
	if !s.blocked.TryRLock() {
		return ErrBlocked
	}
	defer s.blocked.RUnlock()

	for firstChar, filter := range filters {
		data, err := filter.GobEncode()
		if err != nil {
			return err
		}

		err = s.db.Set([]byte{0x02, firstChar}, data, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetFilter gets the bloom filter for the given first package name character
// from the database.
func (s *Store) GetFilter(firstChar byte) (*sbloom.Filter, error) {
	if !s.blocked.TryRLock() {
		return nil, ErrBlocked
	}
	defer s.blocked.RUnlock()

	data, cl, err := s.db.Get([]byte{0x02, firstChar})
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	filter := &sbloom.Filter{}
	err = filter.GobDecode(data)
	if err != nil {
		return nil, err
	}

	return filter, nil
}

// joinTags converts the given tags to bytes, joins them with \x1F as the separator,
// and updates the correct bloom filter for the first character of the package name.
func joinTags(firstChar byte, tags []string, filters map[byte]*sbloom.Filter) []byte {
	if _, ok := filters[firstChar]; !ok {
		filters[firstChar] = sbloom.NewFilter(xxhash.New(), 10)
	}
	out := &bytes.Buffer{}
	for i, tag := range tags {
		btag := unsafeBytes(tag)
		filters[firstChar].Add(btag)
		out.Write(btag)
		if i != len(tags)-1 {
			out.WriteByte(0x1F)
		}
	}
	return out.Bytes()
}

// GetPkg retrieves a package from the store by its name
func (s *Store) GetPkg(name string) (Package, error) {
	if !s.blocked.TryRLock() {
		return Package{}, ErrBlocked
	}
	defer s.blocked.RUnlock()

	data, cl, err := s.db.Get(unsafeBytes(name))
	if err != nil {
		return Package{}, err
	}
	defer cl.Close()

	return Package{
		Name: name,
		Tags: strings.Split(string(data), "\x1F"),
	}, nil
}

func (s *Store) GetPkgNamesByPrefix(prefix string, n int) ([]string, error) {
	if !s.blocked.TryRLock() {
		return nil, ErrBlocked
	}
	defer s.blocked.RUnlock()

	out := make([]string, 0, n)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: unsafeBytes(prefix),
		UpperBound: append(unsafeBytes(prefix[:len(prefix)-1]), prefix[len(prefix)-1]+1),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	i := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if i == n-1 {
			break
		}
		out = append(out, string(iter.Key()))
		i++
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return out, nil
}

// metaKey is the database key for repository metadata
var metaKey = []byte("\x02META")

// RepoMeta represents repository metadata
type RepoMeta struct {
	ETag         string
	LastModified time.Time
}

// WriteMeta writes the repository metadata to the database
func (s *Store) WriteMeta(meta RepoMeta) error {
	if !s.blocked.TryRLock() {
		return ErrBlocked
	}
	defer s.blocked.RUnlock()

	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return s.db.Set(metaKey, data, nil)
}

// GetMeta reads the repository metadata from the database
func (s *Store) GetMeta() (RepoMeta, error) {
	if !s.blocked.TryRLock() {
		return RepoMeta{}, ErrBlocked
	}
	defer s.blocked.RUnlock()

	data, cl, err := s.db.Get(metaKey)
	if err != nil {
		return RepoMeta{}, err
	}
	defer cl.Close()
	var meta RepoMeta
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// Replace atomically replaces the database from s with the database from s2.
// The store is blocked during the replacement, causing any concurrent operations
// to fail with [ErrBlocked]. The replacement operation closes and moves s2's
// database, so s2 is no longer usable after this operation.
//
// This function attempts to roll back in case of partial failures. However, cleanup
// failures may result in leftover temporary files.
func (s *Store) Replace(s2 *Store) error {
	// Clean up any leftover old db files. We don't need to lock at this
	// point because concurrent operations are still safe to execute.
	oldPath := filepath.Join(filepath.Dir(s.Path), "db-old")
	if err := os.RemoveAll(oldPath); err != nil {
		return err
	}

	// Do a write lock, which will prevent any new operations
	// from executing and block until all existing operations
	// complete.
	s.blocked.Lock()

	if err := s2.db.Close(); err != nil {
		s.blocked.Unlock()
		return err
	}
	if err := s.db.Close(); err != nil {
		s.blocked.Unlock()
		return err
	}

	if err := os.Rename(s.Path, oldPath); err != nil {
		s.blocked.Unlock()
		return err
	}
	if err := os.Rename(s2.Path, s.Path); err != nil {
		s.blocked.Unlock()
		return errors.Join(err, os.Rename(oldPath, s.Path))
	}

	db, err := pebble.Open(s.Path, &pebble.Options{Logger: nopLogger{}})
	if err != nil {
		s.blocked.Unlock()
		return err
	}
	s.db = db

	// We can unlock here even though there's more work to do because the replace
	// operation itself is complete and concurrent operations are now safe to
	// execute again.
	s.blocked.Unlock()

	return os.RemoveAll(oldPath)
}

// Close closes the underlying database
func (s *Store) Close() error {
	if !s.blocked.TryRLock() {
		return ErrBlocked
	}
	defer s.blocked.RUnlock()
	return s.db.Close()
}

// overlap calculates the overlap between two sets of tags.
// It returns the list of overlapping tags and a confidence score.
func overlap(stags, ptags []string) ([]string, float32) {
	var overlapTags []string
	for _, stag := range stags {
		if slices.Contains(ptags, stag) {
			overlapTags = append(overlapTags, stag)
		}
	}
	return overlapTags, float32(len(overlapTags)) / float32(len(stags))
}

// unsafeBytes converts a string to a byte slice using unsafe operations
func unsafeBytes(data string) []byte {
	return unsafe.Slice(unsafe.StringData(data), len(data))
}

// unsafeString converts a byte slice to a string using unsafe operations
func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
