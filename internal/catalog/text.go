package catalog

import (
	"net/url"
	"strings"
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

// terminalText removes control characters other than newlines, so catalog
// text cannot drive the terminal.
func terminalText(value string) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "")
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, value)
}

func webURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && plainText(value)
}
