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

package combined

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"go.elara.ws/distrohop/internal/store"
	"golang.org/x/sync/errgroup"
)

var _ store.ReadOnly = (*Store)(nil)

var ErrNotFound = errors.New("no such package")

// Store represents a combined store that aggregates multiple individual [go.elara.ws/distrohop/internal/store.Store] instances.
// It implements [go.elara.ws/distrohop/internal/store.ReadOnly].
type Store struct {
	Stores []store.ReadOnly
}

// New creates a new combined store with the provided individual stores.
func New(stores ...store.ReadOnly) *Store {
	return &Store{stores}
}

// Add adds a new store to the combined store.
func (cs *Store) Add(s store.ReadOnly) {
	cs.Stores = append(cs.Stores, s)
}

// GetPkg retrieves a package by name from any of the stores in the combined store.
// If the package is not found in any store, it returns [github.com/cockroachdb/pebble.ErrNotFound].
func (cs *Store) GetPkg(name string) (out store.Package, err error) {
	mtx := &sync.Mutex{}
	wg := &errgroup.Group{}
	for _, s := range cs.Stores {
		wg.Go(func() error {
			if pkg, err := s.GetPkg(name); err == nil {
				mtx.Lock()
				out = pkg
				mtx.Unlock()
			} else if !errors.Is(err, pebble.ErrNotFound) {
				return err
			}
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		return out, err
	} else if out.Name == "" {
		return out, fmt.Errorf("%w: %q", ErrNotFound, name)
	} else {
		return out, nil
	}
}

// GetPkgNamesByPrefix retrieves package names that match the given prefix from all stores.
// It returns a slice of package names limited to the specified number n.
func (cs *Store) GetPkgNamesByPrefix(prefix string, n int) (out []string, err error) {
	mtx := &sync.Mutex{}
	wg := &errgroup.Group{}
	for _, s := range cs.Stores {
		wg.Go(func() error {
			names, err := s.GetPkgNamesByPrefix(prefix, n)
			if err != nil {
				return err
			}
			mtx.Lock()
			out = append(out, names...)
			mtx.Unlock()
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		return nil, err
	}
	slices.Sort(out)
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Search searches for packages across all stores based on the provided tags.
// It returns a slice of search results and an error.
func (cs *Store) Search(tags []string) (out []store.TagResult, latency time.Duration, err error) {
	mtx := &sync.Mutex{}
	wg := &errgroup.Group{}
	for _, s := range cs.Stores {
		wg.Go(func() error {
			results, dur, err := s.Search(tags)
			if err != nil {
				return err
			}
			mtx.Lock()
			latency += dur
			out = append(out, results...)
			mtx.Unlock()
			return nil
		})
	}
	if err := wg.Wait(); err != nil {
		return nil, latency, err
	} else {
		store.SortResults(out)
		return out, latency, nil
	}
}
