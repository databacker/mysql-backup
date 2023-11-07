package util

import (
	"net/url"
	"strings"
)

// smartParse parse a url, but convert "/" into "file:///"
func SmartParse(raw string) (*url.URL, error) {
	if strings.HasPrefix(raw, "/") {
		raw = "file://" + raw
	}

	return url.Parse(raw)
}
