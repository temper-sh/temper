package upstreamrelease

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func discoveryFixture(t *testing.T) (GitHubSource, *memoryReader, map[string]any) {
	return taggedDiscoveryFixture(t, "example/tool", "b12")
}

func taggedDiscoveryFixture(t *testing.T, repo, tag string) (GitHubSource, *memoryReader, map[string]any) {
	t.Helper()
	archive := makeArchive(t, []tarEntry{{name: "bundle/bin/server", body: "binary", mode: 0o755, kind: tar.TypeReg}})
	asset := "https://github.com/" + repo + "/releases/download/" + tag + "/tool-" + tag + ".tar.gz"
	metadata := map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "assets": []map[string]any{{"name": "tool-" + tag + ".tar.gz", "browser_download_url": asset, "size": len(archive.data), "digest": "sha256:" + archive.sha256}}}
	data, _ := json.Marshal(metadata)
	ref, _ := json.Marshal(map[string]any{"object": map[string]string{"type": "commit", "sha": strings.Repeat("a", 40)}})
	reader := &memoryReader{content: map[string][]byte{
		"https://api.github.com/repos/" + repo + "/releases/latest":      data,
		"https://api.github.com/repos/" + repo + "/releases/tags/" + tag: data,
		"https://api.github.com/repos/" + repo + "/git/ref/tags/" + tag:  ref,
		asset: archive.data,
	}}
	return GitHubSource{Repository: repo, Asset: "tool-{version}.tar.gz", ArchiveRoot: "bundle"}, reader, metadata
}

func TestLlamaCPPLatestSelectsDownloadableNightlyInsteadOfSemanticReleaseHeading(t *testing.T) {
	source, reader, metadata := taggedDiscoveryFixture(t, "ggml-org/llama.cpp", "b11132")
	metadata["prerelease"] = true
	endpoint := "https://api.github.com/repos/ggml-org/llama.cpp"
	reader.content[endpoint+"/releases/latest"] = []byte(`{"tag_name":"v0.4.1","assets":[]}`)
	reader.content[endpoint+"/releases?per_page=20&page=1"], _ = json.Marshal([]any{
		map[string]any{"tag_name": "v0.4.1", "assets": []any{}},
		map[string]any{"tag_name": "b11134", "draft": true, "assets": []any{map[string]any{"name": "tool-b11134.tar.gz"}}},
		map[string]any{"tag_name": "b11133", "prerelease": true, "assets": []any{}},
		metadata,
	})
	reader.content[endpoint+"/releases/tags/b11132"], _ = json.Marshal(metadata)
	for _, requested := range []string{"latest", "b11132"} {
		release, err := Discover(context.Background(), reader, source, requested)
		if err != nil {
			t.Fatalf("%s: %v", requested, err)
		}
		if release.Version != "b11132" || release.Revision != strings.Repeat("a", 40) || release.Artifact.UnpackedSize != 6 {
			t.Fatalf("%s lost exact binary build: %+v", requested, release)
		}
	}
}

func TestLlamaCPPLatestDoesNotFallBackPastAnInvalidSelectedArchive(t *testing.T) {
	source, reader, metadata := taggedDiscoveryFixture(t, "ggml-org/llama.cpp", "b11132")
	_, olderReader, olderMetadata := taggedDiscoveryFixture(t, "ggml-org/llama.cpp", "b11131")
	for locator, data := range olderReader.content {
		reader.content[locator] = data
	}
	metadata["prerelease"] = true
	metadata["assets"].([]map[string]any)[0]["digest"] = "sha256:" + strings.Repeat("0", 64)
	reader.content["https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=20&page=1"], _ = json.Marshal([]any{metadata, olderMetadata})
	if _, err := Discover(context.Background(), reader, source, "latest"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("invalid nightly was not refused: %v", err)
	}
}

func TestLlamaCPPLatestFindsTargetOnNextReleasePage(t *testing.T) {
	source, reader, metadata := taggedDiscoveryFixture(t, "ggml-org/llama.cpp", "b11132")
	metadata["prerelease"] = true
	firstPage := make([]map[string]any, 20)
	for i := range firstPage {
		firstPage[i] = map[string]any{"tag_name": fmt.Sprintf("b%d", 11152-i), "prerelease": true, "assets": []any{}}
	}
	endpoint := "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=20&page="
	reader.content[endpoint+"1"], _ = json.Marshal(firstPage)
	reader.content[endpoint+"2"], _ = json.Marshal([]any{metadata})
	release, err := Discover(context.Background(), reader, source, "latest")
	if err != nil || release.Version != "b11132" {
		t.Fatalf("paginated target discovery: %+v, %v", release, err)
	}
}

func TestLlamaCPPLatestBoundsDiscoveryWhenTargetIsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pages, length int
	}{
		{"end of release list", 1, 1},
		{"discovery limit", 5, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, reader, _ := taggedDiscoveryFixture(t, "ggml-org/llama.cpp", "b11132")
			for page := 1; page <= tc.pages; page++ {
				releases := make([]map[string]any, tc.length)
				for i := range releases {
					releases[i] = map[string]any{"tag_name": fmt.Sprintf("b%d", 11200-page*20-i), "prerelease": true, "assets": []any{}}
				}
				endpoint := fmt.Sprintf("https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=20&page=%d", page)
				reader.content[endpoint], _ = json.Marshal(releases)
			}
			if _, err := Discover(context.Background(), reader, source, "latest"); err == nil || !strings.Contains(err.Error(), "no recent published llama.cpp build") {
				t.Fatalf("missing target did not end bounded discovery: %v", err)
			}
			if reader.opens != tc.pages {
				t.Fatalf("missing target read %d resources, want %d metadata pages", reader.opens, tc.pages)
			}
		})
	}
}

func TestPrereleaseExceptionRequiresAnOfficialLlamaCPPBuild(t *testing.T) {
	for _, tc := range []struct{ repo, tag string }{
		{"ggml-org/llama.cpp", "v0.4.1"},
		{"example/tool", "b11132"},
		{"mostlygeek/llama-swap", "v257"},
	} {
		t.Run(tc.repo+"/"+tc.tag, func(t *testing.T) {
			source, reader, metadata := taggedDiscoveryFixture(t, tc.repo, tc.tag)
			metadata["prerelease"] = true
			reader.content["https://api.github.com/repos/"+tc.repo+"/releases/tags/"+tc.tag], _ = json.Marshal(metadata)
			if _, err := Discover(context.Background(), reader, source, tc.tag); err == nil || !strings.Contains(err.Error(), "eligible requested release") {
				t.Fatalf("accepted unrelated prerelease: %v", err)
			}
		})
	}
}

func TestSemanticReleaseTagsAreOrderedWithinTheirOwnFamily(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		want        int
	}{
		{"v0.4.1", "v0.4.1", 0},
		{"v0.4.10", "v0.4.9", 1},
		{"v0.10.0", "v0.9.99", 1},
		{"v1.0.0", "v0.99.99", 1},
		{"v0.4.0", "v0.4.1", -1},
	} {
		if order, err := CompareVersions(tc.left, tc.right); err != nil || order != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, %v", tc.left, tc.right, order, err)
		}
	}
	for _, other := range []string{"b11132", "v257", "main", "v0.4.1-rc1", "v00.4.1"} {
		if _, err := CompareVersions("v0.4.1", other); err == nil {
			t.Errorf("accepted incompatible or invalid version %q", other)
		}
	}
	source, reader, _ := taggedDiscoveryFixture(t, "example/tool", "v0.4.1")
	for _, requested := range []string{"latest", "v0.4.1"} {
		if release, err := Discover(context.Background(), reader, source, requested); err != nil || release.Version != "v0.4.1" {
			t.Fatalf("semantic %s discovery: %+v, %v", requested, release, err)
		}
	}
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

func TestUnprefixedSplashSemanticReleaseKeepsExactTagAndInventory(t *testing.T) {
	source, reader, _ := taggedDiscoveryFixture(t, "incoai/splash", "1.1.0")
	for _, requested := range []string{"latest", "1.1.0"} {
		release, err := Discover(context.Background(), reader, source, requested)
		if err != nil {
			t.Fatal(err)
		}
		if release.Version != "1.1.0" || !strings.Contains(release.Artifact.Locator, "/1.1.0/") {
			t.Fatal(release)
		}
	}
	if order, err := CompareVersions("1.1.0", "v1.0.9"); err != nil || order <= 0 {
		t.Fatal(order, err)
	}
	if _, err := CompareVersions("1.1.0", "v257"); err == nil {
		t.Fatal("mixed release families accepted")
	}
}
