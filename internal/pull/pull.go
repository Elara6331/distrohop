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

package pull

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeebo/sbloom"
	"go.elara.ws/distrohop/internal/index"
	"go.elara.ws/distrohop/internal/store"
)

const batchSize = 5000

// ErrUpToDate is returned when a repository index is already
// up to date and doesn't require a pull.
var ErrUpToDate = errors.New("repository is already up to date")

// Options represents settings for pull operations
type Options struct {
	BaseURL      string
	Version      string
	Repo         string
	Architecture string
	ProgressFunc func(title string, received, total int64)
}

// progressReader keeps track of download progress and calls
// progressFn with the current progress data.
type progressReader struct {
	r          io.Reader
	title      string
	received   int64
	total      int64
	progressFn func(title string, received, total int64)
}

func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if err != nil {
		return n, err
	}
	pr.received += int64(n)
	pr.progressFn(pr.title, pr.received, pr.total)
	return n, nil
}

// Pull synchronizes a repository index from a remote repository and atomically updates the store.
// If the index is already up to date, it returns [ErrUpToDate]. If opts.ProgressFunc is set,
// Pull will call it continuously with the current progress of the pull operation. The original store
// remains usable and unmodified until the pull operation completes successfully. It will only be
// blocked for the duration of the atomic replacement operation.
func Pull(opts Options, s *store.Store, importer index.Importer) error {
	indexURLs, err := importer.IndexURL(opts.BaseURL, opts.Version, opts.Repo, opts.Architecture)
	if err != nil {
		return err
	}

	var (
		res  *http.Response
		errs []error
	)
	for _, indexURL := range indexURLs {
		ires, err := http.Get(indexURL)
		if err != nil {
			continue
		}

		if ires.StatusCode != 200 {
			errs = append(errs, fmt.Errorf("http: %s", ires.Status))
			continue
		} else {
			res = ires
			break
		}
	}

	if res == nil {
		return errors.Join(errs...)
	} else {
		defer res.Body.Close()
	}

	repoKey := strings.Trim(opts.Version+"/"+opts.Repo+"/"+opts.Architecture, "/")

	if meta, err := s.GetMeta(); err == nil {
		// If the ETag stored in the database is the same as the one we got from the
		// HTTP response, the repo is up to date.
		if etag := res.Header.Get("ETag"); etag != "" && etag == meta.ETag {
			return ErrUpToDate
		}

		if lastModStr := res.Header.Get("Last-Modified"); lastModStr != "" && !meta.LastModified.IsZero() {
			lastMod, err := time.Parse(time.RFC1123, lastModStr)
			// If the last modified time from the HTTP response is before
			// or equal to the time in the database, the repo is up to date.
			if err == nil && meta.LastModified.Compare(lastMod) >= 0 {
				return ErrUpToDate
			}
		}
	}

	dir, err := os.MkdirTemp(filepath.Dir(s.Path), "distrohop-pull.*")
	if err != nil {
		return err
	}

	s2, err := store.Open(dir)
	if err != nil {
		return err
	}

	var r io.Reader = res.Body
	if opts.ProgressFunc != nil {
		r = &progressReader{
			r:          res.Body,
			title:      repoKey,
			total:      res.ContentLength,
			progressFn: opts.ProgressFunc,
		}
	}

	out := make(chan index.Record)
	go importer.ReadPkgData(r, out)

	filters := map[byte]*sbloom.Filter{}

	i := 0
	collected := make(map[string]index.Record, batchSize)
	for rec := range out {
		if rec.Error != nil {
			return rec.Error
		}

		curRec, ok := collected[rec.Name]
		if !ok {
			collected[rec.Name] = rec
		} else {
			curRec.Tags = append(curRec.Tags, rec.Tags...)
			collected[rec.Name] = curRec
		}

		if i >= batchSize {
			err = s2.WriteBatch(collected, filters)
			if err != nil {
				return err
			}
			clear(collected)
			i = 0
		}

		i++
	}

	if len(collected) != 0 {
		err = s2.WriteBatch(collected, filters)
		if err != nil {
			return err
		}
	}

	err = s2.WriteFilters(filters)
	if err != nil {
		return err
	}

	meta := store.RepoMeta{ETag: res.Header.Get("ETag")}

	if lastMod := res.Header.Get("Last-Modified"); lastMod != "" {
		meta.LastModified, err = time.Parse(time.RFC1123, lastMod)
		if err != nil {
			return err
		}
	}

	if err := s2.WriteMeta(meta); err != nil {
		return err
	}

	return s.Replace(s2)
}
