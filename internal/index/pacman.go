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
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/mholt/archives"
	"go.elara.ws/distrohop/internal/tags"
)

type Pacman struct{}

func (Pacman) Name() string {
	return "pacman"
}

func (Pacman) IndexURL(baseURL, version, repo, arch string) ([]string, error) {
	baseURL = os.Expand(baseURL, func(s string) string {
		switch s {
		case "repo":
			return repo
		case "arch":
			return arch
		}
		return "$" + s
	})

	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, err
	}
	filePath, err := url.JoinPath(u.Path, repo+".files")
	if err != nil {
		return nil, err
	}
	u.Path = filePath
	return []string{u.String()}, nil
}

func (Pacman) ReadPkgData(r io.Reader, out chan Record) {
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

	tr := tar.NewReader(dr)
	var currentPkg string

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			close(out)
			break
		} else if err != nil {
			out <- Record{Error: err}
			return
		}

		switch path.Base(hdr.Name) {
		case "desc":
			data, err := io.ReadAll(tr)
			if err != nil {
				out <- Record{Error: err}
				return
			}

			labelIdx := bytes.Index(data, []byte("%NAME%\n"))
			if labelIdx == -1 {
				continue
			}

			start := labelIdx + 7
			end := start + bytes.IndexByte(data[start:], '\n')
			currentPkg = string(data[start:end])
		case "files":
			br := bufio.NewReader(tr)
			for {
				fpath, err := br.ReadString('\n')
				if errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					out <- Record{Error: err}
					return
				}

				fpath = strings.TrimSpace(fpath)
				if fpath == "%FILES%" || strings.HasSuffix(fpath, "/") {
					continue
				}

				fpath = "/" + fpath

				out <- Record{
					Name: currentPkg,
					Tags: tags.Generate(fpath),
				}
			}
		}
	}
}
