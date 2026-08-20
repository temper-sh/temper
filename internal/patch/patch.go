// Package patch parses pinned patch sources and applies Temper-owned,
// deterministic source transforms.
package patch

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const Qwen38PrefixStabilityV1 = "qwen38-prefix-stability-v1"

var (
	namePattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
)

type Source struct {
	Repo      string
	Revision  string
	File      string
	Transform string
}

func ParseSource(value string) (Source, error) {
	if strings.Contains(value, "%") {
		return Source{}, errors.New("patch source must use an unescaped canonical URL")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return Source{}, fmt.Errorf("parse patch source: %w", err)
	}
	if parsed.Scheme != "hf" || parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" || !namePattern.MatchString(parsed.Host) {
		return Source{}, errors.New("patch source must be hf://owner/repo@commit/path")
	}
	trimmed := strings.TrimPrefix(parsed.Path, "/")
	firstSlash := strings.IndexByte(trimmed, '/')
	if firstSlash <= 0 || firstSlash == len(trimmed)-1 {
		return Source{}, errors.New("patch source must include repository and file path")
	}
	repoRevision := trimmed[:firstSlash]
	separator := strings.LastIndexByte(repoRevision, '@')
	if separator <= 0 || separator == len(repoRevision)-1 {
		return Source{}, errors.New("patch source must pin a repository commit")
	}
	repository := repoRevision[:separator]
	revision := repoRevision[separator+1:]
	file := trimmed[firstSlash+1:]
	if !namePattern.MatchString(repository) || !revisionPattern.MatchString(revision) || !safeRelativePath(file) {
		return Source{}, errors.New("patch source has an invalid repository, commit, or file path")
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Source{}, fmt.Errorf("parse patch source query: %w", err)
	}
	if len(query) > 1 {
		return Source{}, errors.New("patch source only supports the transform query parameter")
	}
	var transform string
	if values, ok := query["transform"]; ok {
		if len(values) != 1 || !idPattern.MatchString(values[0]) {
			return Source{}, errors.New("patch source transform must be one stable id")
		}
		transform = values[0]
		if transform != Qwen38PrefixStabilityV1 {
			return Source{}, fmt.Errorf("unsupported patch transform %q", transform)
		}
	} else if len(query) != 0 {
		return Source{}, errors.New("patch source only supports the transform query parameter")
	}

	return Source{
		Repo:      parsed.Host + "/" + repository,
		Revision:  revision,
		File:      file,
		Transform: transform,
	}, nil
}

func Apply(transform string, source []byte) ([]byte, error) {
	switch transform {
	case "":
		return bytes.Clone(source), nil
	case Qwen38PrefixStabilityV1:
		return replaceExactlyOnce(source,
			[]byte("{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"),
			[]byte("{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and (reasoning_content or not ns_state.thinking) %}"),
		)
	default:
		return nil, fmt.Errorf("unsupported patch transform %q", transform)
	}
}

func replaceExactlyOnce(source, old, replacement []byte) ([]byte, error) {
	if count := bytes.Count(source, old); count != 1 {
		return nil, fmt.Errorf("patch transform expected exactly one source match, found %d", count)
	}
	return bytes.Replace(source, old, replacement, 1), nil
}

func safeRelativePath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
