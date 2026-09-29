package catalog

import (
	"net/url"
	"unicode"
	"unicode/utf8"
)

func plainText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			return false
		}
	}
	return true
}

func webURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && plainText(value)
}
