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
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
)

 type Zypper struct{}
 
 func (Zypper) Name() string {
	return "zypper"
 }
 
 func (Zypper) IndexURL(baseURL, version, repo, _ string) ([]string, error) {
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, err
	}
	
	repomdURL := u.JoinPath(version, "repo", repo, "repodata/repomd.xml")
	res, err := http.Get(repomdURL.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
 
	var data repomd
	err = xml.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return nil, err
	}
 
	gzipFile := data.getFilelists()
	if gzipFile == "" {
		return nil, errors.New("no filelists found in repomd.xml")
	}
 
	filelistURL := u.JoinPath(version, "repo", repo, gzipFile)
	return []string{filelistURL.String()}, nil
 }
 
 func (Zypper) ReadPkgData(r io.Reader, out chan Record) {
 	DNF{}.ReadPkgData(r, out)
 }