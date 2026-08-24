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

// ResolvedUnit is the provider-neutral form of one exact installable unit.
// Provider-specific values stop at the adapter boundary before this value is
// returned to selection policy.
type ResolvedUnit struct {
	Scope        string
	NativeName   string
	Version      string
	Revision     string
	Dependencies []string
	Artifacts    []Artifact
}

// Candidate is one exact root and its complete dependency closure as observed
// by a provider adapter. Current is meaningful only for an opaque version
// scheme, where the provider rather than Temper defines "latest".
type Candidate struct {
	RootUnit string
	Units    map[string]ResolvedUnit
	Current  bool
}
