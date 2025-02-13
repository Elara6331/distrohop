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

package cached

import (
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"go.elara.ws/distrohop/internal/store"
)

var _ store.ReadOnly = (*Store)(nil)

// cacheRecord represents a single item stored in the cache
type cacheRecord struct {
	results []store.TagResult
	latency time.Duration
}

// Store represents a cached store that caches search results from [go.elara.ws/distrohop/internal/store.ReadOnly] instances.
// It implements [go.elara.ws/distrohop/internal/store.ReadOnly].
type Store struct {
	store.ReadOnly
	cache *cache.Cache
}

// New creates a new cached store with the provided cache settings and underlying store.
func New(s store.ReadOnly, exp, cleanup time.Duration) Store {
	return Store{
		ReadOnly: s,
		cache:    cache.New(exp, cleanup),
	}
}

// Search retrieves cached search results for the given tags. If the search doesn't exist
// in the cache, it queries the underlying store and adds the results to the cache.
func (cs Store) Search(tags []string) ([]store.TagResult, time.Duration, error) {
	cacheKey := strings.Join(tags, "\x1F")
	if results, ok := cs.cache.Get(cacheKey); ok {
		record := results.(cacheRecord)
		return record.results, record.latency, nil
	}
	res, latency, err := cs.ReadOnly.Search(tags)
	if err != nil {
		return nil, 0, err
	}
	if len(res) != 0 {
		cs.cache.Set(cacheKey, cacheRecord{res, latency}, cache.DefaultExpiration)
	}
	return res, latency, nil
}
