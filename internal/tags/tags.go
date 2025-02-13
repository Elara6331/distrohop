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

package tags

import (
	"path"
	"strings"
)

// Generate generates a list of tags based on the input filename.
func Generate(filePath string) (tags []string) {
	lastSlash := strings.LastIndexByte(filePath, '/')
	name, dir := filePath[lastSlash+1:], filePath[:lastSlash]
	pathElems := strings.Split(dir, "/")
	added := false
	for _, elem := range pathElems {
		switch elem {
		case "usr", "opt", "local", "share":
			// Skip directories that we don't care about
			continue
		case "bin", "sbin":
			tags = append(tags, "bin="+name)
			added = true
		case "icons", "pixmaps":
			switch path.Ext(name) {
			case ".svg", ".png", ".jpg", ".jpeg":
				tags = append(tags, "icon="+name)
				added = true
			}
		case "man":
			if manName := manualName(name); manName != "" {
				tags = append(tags, "man="+manName)
				added = true
			}
		case "dist-packages", "site-packages":
			if pyName := pythonName(filePath); pyName != "" {
				tags = append(tags, "py="+pyName)
				added = true
			}
		case "pkgconfig", "pkg-config":
			if path.Ext(name) == ".pc" {
				tags = append(tags, "pkgcfg="+strings.TrimSuffix(name, ".pc"))
				added = true
			}
		case "applications":
			if path.Ext(name) == ".desktop" {
				tags = append(tags, "desktop="+strings.TrimSuffix(name, ".desktop"))
				added = true
			}
		case "dbus-1":
			if path.Ext(name) == ".service" {
				tags = append(tags, "dbus="+strings.TrimSuffix(name, ".service"))
				added = true
			}
		case "systemd":
			switch path.Ext(name) {
			case ".service", ".target", ".socket", ".timer":
				tags = append(tags, "systemd="+name)
				added = true
			}
		case "include":
			switch path.Ext(name) {
			case ".h", ".hh", ".hpp", ".hxx", "h++":
				_, hdrName, ok := strings.Cut(filePath, "include/")
				if !ok {
					hdrName = name
				}
				tags = append(tags, "hdr="+hdrName)
				added = true
			}
		case "lib", "lib32", "lib64":
			if libName, soversion, ok := strings.Cut(name, ".so"); ok && soversionIsValid(soversion) {
				tags = append(tags, "lib="+name)
				lastChar := name[len(name)-1]
				if lastChar >= '0' || lastChar <= '9' {
					tags = append(tags, "lib="+libName+".so")
					canonicalLibName := strings.TrimPrefix(libName, "lib")
					tags = append(tags, "lib="+canonicalLibName)
				}
				added = true
			} else if path.Ext(name) == ".a" {
				tags = append(tags, "lib="+name)
				tags = append(tags, "lib="+strings.TrimSuffix(name, ".a"))
				added = true
			}
		default:
			continue
		}

		if added {
			break
		}
	}

	if !added {
		tags = append(tags, "file="+filePath)
	}

	return tags
}

func manualName(fileName string) string {
	fileName = strings.TrimSuffix(fileName, ".gz")
	ext := path.Ext(fileName)
	if len(ext) == 0 || !isNum(ext[1:]) {
		return ""
	}
	return fileName
}

func pythonName(filePath string) string {
	for _, start := range [...]string{"/dist-packages/", "/site-packages/"} {
		start := strings.Index(filePath, start)
		if start == -1 {
			continue
		}
		start += 15
		end := strings.Index(filePath[start:], "/")
		if end == -1 {
			continue
		}
		return filePath[start : start+end]
	}
	return ""
}

func soversionIsValid(s string) bool {
	if s == "" {
		return true
	}

	for _, elem := range strings.Split(s, ".") {
		if !isNum(elem) {
			return false
		}
	}

	return true
}

func isNum(s string) bool {
	for i := range s {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
