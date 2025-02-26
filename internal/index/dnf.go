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

package index

import (
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mholt/archives"
	"go.elara.ws/distrohop/internal/tags"
)

type repomd struct {
	Locations []location `xml:"data>location"`
}

type location struct {
	Href string `xml:"href,attr"`
}

func (r repomd) getGzipFile() string {
	for _, loc := range r.Locations {
		if strings.HasSuffix(loc.Href, "filelists.xml.gz") {
			return loc.Href
		}
	}
	return ""
}

type DNF struct{}

func (DNF) Name() string {
	return "dnf"
}

func (DNF) IndexURL(baseURL, version, repo, arch string) ([]string, error) {
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, err
	}
	repomdPath := fmt.Sprintf("/pub/fedora/linux/releases/%s/%s/%s/os/repodata/repomd.xml", version, repo, arch)
	u.Path = repomdPath

	res, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var data repomd
	err = xml.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	gzipFile := data.getGzipFile()
	if gzipFile == "" {
		return nil, errors.New("no gzip file found in repomd.xml")
	}

	u.Path = fmt.Sprintf("/pub/fedora/linux/releases/%s/%s/%s/os/%s", version, repo, arch, gzipFile)
	return []string{u.String()}, nil
}

func (DNF) ReadPkgData(r io.Reader, out chan Record) {
	ctx := context.Background()
	format, r, err := archives.Identify(ctx, "", r)
	if err != nil {
		out <- Record{Error: err}
		return
	}

	decomp, ok := format.(archives.Decompressor)
	if !ok {
		out <- Record{Error: errors.New("downloaded index is not a valid compressed file")}
		return
	}

	dr, err := decomp.OpenReader(r)
	if err != nil {
		out <- Record{Error: err}
		return
	}
	defer dr.Close()

	br := bufio.NewReader(dr)
	var currentPkg string

	for {
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) {
			close(out)
			break
		} else if err != nil {
			out <- Record{Error: err}
			return
		}
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "<file"):
			// Skip directories and symlinks
			if strings.HasPrefix(line[5:], `type="dir"`) || line[5] == 'l' {
				continue
			}

			start := strings.IndexByte(line, '>') + 1
			end := strings.LastIndexByte(line, '<')
			fpath := line[start:end]

			if strings.Contains(fpath, ".build-id") {
				continue
			}

			out <- Record{
				Name: currentPkg,
				Tags: tags.Generate(fpath),
			}
		case strings.HasPrefix(line, "<package"):
			start := strings.LastIndex(line, `name="`) + 6
			end := start + strings.IndexByte(line[start:], '"')
			currentPkg = line[start:end]
		default:
			continue
		}
	}
}
