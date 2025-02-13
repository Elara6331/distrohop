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
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/mholt/archives"
	"go.elara.ws/distrohop/internal/tags"
)

type APT struct{}

func (APT) Name() string {
	return "apt"
}

func (APT) IndexURL(baseURL, version, repo, arch string) ([]string, error) {
	indexURL, err := url.JoinPath(baseURL, "dists", version, repo, "Contents-"+arch+".gz")
	if err != nil {
		return nil, err
	}
	// Before Debian Wheezy, the path to Contents indices didn't include $COMP/repo, so we need to try
	// both the new and old URL formats. Ubuntu also still uses the pre-Debian-Wheezy convention.
	deprecatedURL, err := url.JoinPath(baseURL, "dists", version, "Contents-"+arch+".gz")
	if err != nil {
		return nil, err
	}
	return []string{indexURL, deprecatedURL}, nil
}

func (APT) ReadPkgData(r io.Reader, out chan Record) {
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
	for {
		line, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) {
			close(out)
			break
		} else if err != nil {
			out <- Record{Error: err}
			return
		}

		lastSpaceIdx := strings.LastIndexByte(line, ' ')
		if lastSpaceIdx == -1 {
			continue
		}

		fpath := "/" + strings.TrimSpace(line[:lastSpaceIdx])
		names := strings.Split(strings.TrimSpace(line[lastSpaceIdx+1:]), ",")
		for _, name := range names {
			slashIdx := strings.LastIndexByte(name, '/')
			if slashIdx != -1 {
				name = name[slashIdx+1:]
			}

			if strings.Contains(fpath, "changelog.Debian") ||
				strings.Contains(fpath, "README.Debian") ||
				strings.Contains(fpath, "NEWS.Debian.gz") {
				continue
			}

			out <- Record{
				Name: name,
				Tags: tags.Generate(fpath),
			}
		}
	}
}
