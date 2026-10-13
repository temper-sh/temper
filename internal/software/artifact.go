package software

// Artifact identifies immutable provider content used by one resolved unit.
type Artifact struct {
	Locator          string `yaml:"locator" json:"locator"`
	SHA256           string `yaml:"sha256" json:"sha256"`
	Size             int64  `yaml:"size,omitempty" json:"size,omitempty"`
	UnpackedSize     int64  `yaml:"unpacked_size,omitempty" json:"unpacked_size,omitempty"`
	InstalledEntries int    `yaml:"installed_entries,omitempty" json:"installed_entries,omitempty"`
	Format           string `yaml:"format,omitempty" json:"format,omitempty"`
	ArchiveRoot      string `yaml:"archive_root,omitempty" json:"archive_root,omitempty"`
}
