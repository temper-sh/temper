package releaseartifact_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/releaseartifact"
)

func TestBuildArchiveIsDeterministicAndExact(t *testing.T) {
	t.Parallel()
	files := []releaseartifact.File{
		{Name: "THIRD_PARTY_NOTICES.txt", Data: []byte("notices\n")},
		{Name: "temper", Data: []byte("binary")},
		{Name: "LICENSE", Data: []byte("0BSD\n")},
	}
	archive, identity, checksum, err := releaseartifact.BuildArchive("0.1.0-alpha.1", files)
	if err != nil {
		t.Fatal(err)
	}
	reordered := []releaseartifact.File{files[1], files[2], files[0]}
	again, againIdentity, againChecksum, err := releaseartifact.BuildArchive("0.1.0-alpha.1", reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, again) || identity != againIdentity || !bytes.Equal(checksum, againChecksum) {
		t.Fatal("release archive changed when inputs were reordered")
	}

	digest := sha256.Sum256(archive)
	if got, want := identity, hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
	if got, want := string(checksum), identity+"  temper_0.1.0-alpha.1_darwin_arm64.zip\n"; got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{
		"temper_0.1.0-alpha.1_darwin_arm64/temper",
		"temper_0.1.0-alpha.1_darwin_arm64/LICENSE",
		"temper_0.1.0-alpha.1_darwin_arm64/THIRD_PARTY_NOTICES.txt",
	}
	wantModes := []uint32{0o755, 0o644, 0o644}
	wantData := []string{"binary", "0BSD\n", "notices\n"}
	if len(zr.File) != len(wantNames) {
		t.Fatalf("ZIP entries = %d, want %d", len(zr.File), len(wantNames))
	}
	for index, file := range zr.File {
		if file.Name != wantNames[index] {
			t.Errorf("entry %d name = %q, want %q", index, file.Name, wantNames[index])
		}
		if got := uint32(file.Mode().Perm()); got != wantModes[index] {
			t.Errorf("entry %q mode = %#o, want %#o", file.Name, got, wantModes[index])
		}
		if got, want := file.Modified.UTC(), time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("entry %q time = %s, want %s", file.Name, got, want)
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %q: %v; close: %v", file.Name, readErr, closeErr)
		}
		if got := string(data); got != wantData[index] {
			t.Errorf("entry %q data = %q, want %q", file.Name, got, wantData[index])
		}
	}
}

func TestBuildArchiveRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	valid := []releaseartifact.File{
		{Name: "temper", Data: []byte("binary")},
		{Name: "LICENSE", Data: []byte("license")},
		{Name: "THIRD_PARTY_NOTICES.txt", Data: []byte("notices")},
	}
	tests := []struct {
		name    string
		version string
		files   []releaseartifact.File
	}{
		{name: "tag prefix", version: "v1.2.3", files: valid},
		{name: "leading zero", version: "01.2.3", files: valid},
		{name: "missing", version: "1.2.3", files: valid[:2]},
		{name: "unknown", version: "1.2.3", files: append(valid, releaseartifact.File{Name: "README", Data: []byte("x")})},
		{name: "duplicate", version: "1.2.3", files: append(valid, valid[0])},
		{name: "empty", version: "1.2.3", files: []releaseartifact.File{{Name: "temper"}, valid[1], valid[2]}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, err := releaseartifact.BuildArchive(test.version, test.files); err == nil {
				t.Fatal("BuildArchive succeeded, want error")
			}
		})
	}
}

func TestBuildNoticesSortsModulesAndFiles(t *testing.T) {
	t.Parallel()
	notices, err := releaseartifact.BuildNotices([]releaseartifact.ModuleNotice{
		{Path: "example.com/z", Version: "v2.0.0", Files: []releaseartifact.NoticeFile{{Name: "NOTICE", Data: []byte("notice")}, {Name: "LICENSE", Data: []byte("license\n")}}},
		{Path: "example.com/a", Version: "v1.0.0", Files: []releaseartifact.NoticeFile{{Name: "COPYING.txt", Data: []byte("copying")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Third-party notices for Temper\n\n" +
		"The Temper source tree is licensed under 0BSD. The release binary also\n" +
		"contains the following Go modules under their respective terms.\n" +
		"\n================================================================================\n" +
		"MODULE example.com/a v1.0.0\n" +
		"\nFILE COPYING.txt\n" +
		"--------------------------------------------------------------------------------\n" +
		"copying\n" +
		"\n================================================================================\n" +
		"MODULE example.com/z v2.0.0\n" +
		"\nFILE LICENSE\n" +
		"--------------------------------------------------------------------------------\n" +
		"license\n" +
		"\nFILE NOTICE\n" +
		"--------------------------------------------------------------------------------\n" +
		"notice\n"
	if got := string(notices); got != want {
		t.Fatalf("notices differ:\n%s", got)
	}
}

func TestBuildNoticesRejectsMissingOrInvalidMaterial(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		modules []releaseartifact.ModuleNotice
	}{
		{name: "empty"},
		{name: "missing files", modules: []releaseartifact.ModuleNotice{{Path: "example.com/a", Version: "v1.0.0"}}},
		{name: "path", modules: []releaseartifact.ModuleNotice{{Path: "example.com/a", Version: "v1.0.0", Files: []releaseartifact.NoticeFile{{Name: "../LICENSE", Data: []byte("x")}}}}},
		{name: "not a notice", modules: []releaseartifact.ModuleNotice{{Path: "example.com/a", Version: "v1.0.0", Files: []releaseartifact.NoticeFile{{Name: "README", Data: []byte("x")}}}}},
		{name: "duplicate module", modules: []releaseartifact.ModuleNotice{
			{Path: "example.com/a", Version: "v1.0.0", Files: []releaseartifact.NoticeFile{{Name: "LICENSE", Data: []byte("x")}}},
			{Path: "example.com/a", Version: "v1.0.0", Files: []releaseartifact.NoticeFile{{Name: "LICENSE", Data: []byte("x")}}},
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := releaseartifact.BuildNotices(test.modules); err == nil {
				t.Fatal("BuildNotices succeeded, want error")
			}
		})
	}
}
