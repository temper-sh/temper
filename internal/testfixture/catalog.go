package testfixture

import (
	_ "embed"
	"encoding/json"

	"github.com/temper-sh/temper/internal/catalog"
)

// These compositions exercise the historical saved-pair contract. Current
// authoring no longer owns user layouts or imposes local/utility runtime types.
//
//go:embed setup-profiles-v2.json
var setupProfilesV2 []byte

func LegacySetupCatalog(raw []byte) (catalog.Document, error) {
	d, err := catalog.Parse(raw)
	if err != nil {
		return d, err
	}
	d.Profiles = map[string]catalog.Profile{}
	if err = json.Unmarshal(setupProfilesV2, &d.Profiles); err != nil {
		return d, err
	}
	return d, d.Validate()
}
