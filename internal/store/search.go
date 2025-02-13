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
	"encoding/gob"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cockroachdb/pebble"
)

func init() {
	gob.Register(&xxhash.Digest{})
}

var ErrInvalidTag = errors.New("invalid tag format")

var (
	// startChars is a list of all the possible package name starting characters
	startChars = [...]byte{
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	}
	// iterOpts contains iterator options with bounds defined such that
	// they cover all packages starting with each character defined
	// in startChars
	iterOpts = make([]*pebble.IterOptions, len(startChars))
	// tagRegex validates the format of a given tag
	tagRegex = regexp.MustCompile(`\w+=.+`)
)

func init() {
	// Populate the iterOpts slice
	for i, char := range startChars {
		iterOpts[i] = &pebble.IterOptions{
			LowerBound: []byte{char},
			UpperBound: []byte{char + 1},
		}
	}
}

// TagResult represents the result of a tag search, including confidence and overlapping tags.
type TagResult struct {
	// The confidence score for the tag match. This value will always be between 0 and 1.
	Confidence float32
	// A list of overlapping tags
	Overlap []string
	// The package associated with the tag result
	Package Package
}

// Search searches for packages in the store that match the given tags.
// Each tag must be in the format "key=value", and an error is returned
// if any tag does not conform to this format. The function spawns multiple
// worker goroutines (defined by s.SearchThreads) to perform a concurrent search.
// The result is a list of [TagResult] structs representing the matching packages.
func (s *Store) Search(tags []string) ([]TagResult, time.Duration, error) {
	start := time.Now()
	for _, tag := range tags {
		if !tagRegex.MatchString(tag) {
			return nil, 0, fmt.Errorf("%w: %q", ErrInvalidTag, tag)
		}
	}

	optsMtx := &sync.Mutex{}
	opts := iterOpts

	var results []TagResult
	resultsMtx := &sync.Mutex{}
	wg := &sync.WaitGroup{}
	errs := make(chan error)
	for range s.SearchThreads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				optsMtx.Lock()
				if len(opts) == 0 {
					// If we have no more options structs left,
					// we can exit the goroutine
					optsMtx.Unlock()
					return
				}
				opt := opts[0]
				opts = opts[1:]
				optsMtx.Unlock()

				found := false
				if filter, err := s.GetFilter(opt.LowerBound[0]); err == nil {
					for _, tag := range tags {
						if filter.Lookup(unsafeBytes(tag)) {
							found = true
							break
						}
					}
				} else if !errors.Is(err, pebble.ErrNotFound) {
					errs <- err
					return
				}

				// Skip the current chunk if the bloom filter
				// doesn't contain any of the tags, or if it doesn't
				// exist, which indicates that there are no packages
				// with the starting character we're looking for.
				if !found {
					continue
				}

				// Create a new iterator that scans through the range defined in opt
				iter, err := s.db.NewIter(opt)
				if err != nil {
					errs <- err
					return
				}

				var out []TagResult
				for iter.First(); iter.Valid(); iter.Next() {
					val, err := iter.ValueAndErr()
					if err != nil {
						errs <- err
						iter.Close()
						return
					}

					// Convert the tag data to a string using an unsafe operation
					// so that we can split it by the unit separator character
					// and check if it has overlap without incurring the cost
					// of copying the value for a string conversion.
					//
					// If we find that there's overlap, we'll copy the data
					// later, before returning it.
					ptags := strings.Split(unsafeString(val), "\x1F")
					overlapTags, conf := overlap(tags, ptags)
					if conf == 0 {
						// If the confidence is zero, there's no overlap,
						// so we can continue to the next value
						continue
					}

					out = append(out, TagResult{
						Confidence: conf,
						Overlap:    overlapTags,
						Package: Package{
							Name: string(iter.Key()),
							// We need to do a deep copy here because we previously
							// used an unsafe operation to convert the tag data to
							// a string, and the values created by that will be
							// invalidated when the iterator is closed.
							Tags: cloneStringSlice(ptags),
						},
					})
				}

				if err := iter.Error(); err != nil {
					errs <- err
					iter.Close()
					return
				}

				iter.Close()
				resultsMtx.Lock()
				results = append(results, out...)
				resultsMtx.Unlock()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errs:
		if err != nil {
			return nil, 0, err
		}
	case <-done:
		SortResults(results)
		return results, time.Since(start), nil
	}

	SortResults(results)
	return results, time.Since(start), nil
}

// SortResults sorts tag results by confidence
func SortResults(results []TagResult) {
	slices.SortFunc(results, func(a, b TagResult) int {
		if a.Confidence < b.Confidence {
			return 1
		} else if a.Confidence > b.Confidence {
			return -1
		} else {
			return strings.Compare(a.Package.Name, b.Package.Name)
		}
	})
}

// cloneStringSlice creates a deep copy of a slice of strings
func cloneStringSlice(s []string) []string {
	out := make([]string, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = strings.Clone(s[i])
	}
	return out
}
