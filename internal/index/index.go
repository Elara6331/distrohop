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
	"fmt"
	"io"
)

// Record represents a data record for a single package
type Record struct {
	Name  string
	Tags  []string
	Error error
}

type Importer interface {
	// Name returns the name of the importer
	Name() string
	// IndexURL generates a list of possible index URLs to try
	IndexURL(baseURL, version, repo, arch string) ([]string, error)
	// ReadPkgData reads data from an index file and sends it on out
	ReadPkgData(r io.Reader, out chan Record)
}

var importers = []Importer{
	APT{},
	DNF{},
	Pacman{},
}

// GetImporter gets an importer by its name
func GetImporter(name string) (Importer, error) {
	for _, importer := range importers {
		if importer.Name() == name {
			return importer, nil
		}
	}
	return nil, fmt.Errorf("no such importer: %q", name)
}
