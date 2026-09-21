package upstreamrelease

import (
	"archive/tar"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func discoveryFixture(t *testing.T) (GitHubSource, *memoryReader, map[string]any) {
	t.Helper()
	archive := makeArchive(t, []tarEntry{{name: "bundle/bin/server", body: "binary", mode: 0o755, kind: tar.TypeReg}})
	asset := "https://github.com/example/tool/releases/download/b12/tool-b12.tar.gz"
	metadata := map[string]any{"tag_name": "b12", "draft": false, "prerelease": false, "assets": []map[string]any{{"name": "tool-b12.tar.gz", "browser_download_url": asset, "size": len(archive.data), "digest": "sha256:" + archive.sha256}}}
	data, _ := json.Marshal(metadata)
	ref, _ := json.Marshal(map[string]any{"object": map[string]string{"type": "commit", "sha": strings.Repeat("a", 40)}})
	reader := &memoryReader{content: map[string][]byte{
		"https://api.github.com/repos/example/tool/releases/latest":   data,
		"https://api.github.com/repos/example/tool/releases/tags/b12": data,
		"https://api.github.com/repos/example/tool/git/ref/tags/b12":  ref,
		asset: archive.data,
	}}
	return GitHubSource{Repository: "example/tool", Asset: "tool-{version}.tar.gz", ArchiveRoot: "bundle"}, reader, metadata
}

func TestDiscoverResolvesLatestAndExactVersionsWithVerifiedArchiveInventory(t *testing.T) {
	for _, requested := range []string{"latest", "b12"} {
		t.Run(requested, func(t *testing.T) {
			source, reader, _ := discoveryFixture(t)
			r, err := Discover(context.Background(), reader, source, requested)
			if err != nil {
				t.Fatal(err)
			}
			if r.Version != "b12" || r.Revision != strings.Repeat("a", 40) || r.Artifact.UnpackedSize != 6 || r.Artifact.InstalledEntries != 2 || r.Artifact.SHA256 == "" {
				t.Fatalf("incomplete resolved release: %+v", r)
			}
		})
	}
}

func TestDiscoverRefusesIneligibleOrChangedUpstreamAssets(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"prerelease", func(m map[string]any) { m["prerelease"] = true }},
		{"draft", func(m map[string]any) { m["draft"] = true }},
		{"missing target asset", func(m map[string]any) { m["assets"] = []any{} }},
		{"wrong digest", func(m map[string]any) {
			m["assets"].([]map[string]any)[0]["digest"] = "sha256:" + strings.Repeat("0", 64)
		}},
		{"missing digest", func(m map[string]any) { m["assets"].([]map[string]any)[0]["digest"] = "" }},
		{"wrong size", func(m map[string]any) { m["assets"].([]map[string]any)[0]["size"] = 1 }},
		{"foreign archive", func(m map[string]any) {
			m["assets"].([]map[string]any)[0]["browser_download_url"] = "https://example.test/other.tar.gz"
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			source, reader, metadata := discoveryFixture(t)
			tt.edit(metadata)
			reader.content["https://api.github.com/repos/example/tool/releases/latest"], _ = json.Marshal(metadata)
			if _, err := Discover(context.Background(), reader, source, "latest"); err == nil {
				t.Fatal("accepted invalid release")
			}
		})
	}
	source, reader, _ := discoveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, reader, source, "latest"); err == nil || reader.opens != 0 {
		t.Fatalf("cancelled discovery: %v, reads=%d", err, reader.opens)
	}
}

func TestReleaseVersionOrderIsNumericAndRequiresSameTagFamily(t *testing.T) {
	if order, err := CompareVersions("b100", "b99"); err != nil || order <= 0 {
		t.Fatalf("build ordering = %d, %v", order, err)
	}
	for _, other := range []string{"v99", "main", "b100-rc1"} {
		if _, err := CompareVersions("b100", other); err == nil {
			t.Fatalf("guessed order against %s", other)
		}
	}
}
