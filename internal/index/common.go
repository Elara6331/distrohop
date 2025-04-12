package index

import "strings"

type repomd struct {
	Locations []location `xml:"data>location"`
}

type location struct {
	Href string `xml:"href,attr"`
}

func (r repomd) getFilelists() string {
	for _, loc := range r.Locations {
		if strings.Contains(loc.Href, "filelists.xml") {
			return loc.Href
		}
	}
	return ""
}