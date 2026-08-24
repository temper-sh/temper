// Package catalogtrust owns the catalog-signing public keys compiled into
// Temper releases. Private signing material is never a runtime input.
package catalogtrust

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	catalogpublication "github.com/temper-sh/temper/internal/software/catalogpublication"
)

const (
	// ProductionKeyID is the stable identifier carried by publications signed
	// with Temper's first production catalog key.
	ProductionKeyID = "temper-catalog-2026-01"

	productionPublicKeyBase64 = "e+0Mho1R2Sbw/yORbaLUJQk6IdSuiub/Sd5LIU9MBVU="
)

// Production constructs the immutable trust root for official catalog
// publications. A malformed compiled key is a release defect, returned to the
// composition root before any catalog read or activation can occur.
func Production() (catalogpublication.TrustRoot, error) {
	publicKey, err := base64.StdEncoding.Strict().DecodeString(productionPublicKeyBase64)
	if err != nil {
		return catalogpublication.TrustRoot{}, fmt.Errorf("decode production catalog trust key %q: %w", ProductionKeyID, err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return catalogpublication.TrustRoot{}, fmt.Errorf(
			"production catalog trust key %q has length %d, want %d",
			ProductionKeyID,
			len(publicKey),
			ed25519.PublicKeySize,
		)
	}
	if base64.StdEncoding.EncodeToString(publicKey) != productionPublicKeyBase64 {
		return catalogpublication.TrustRoot{}, fmt.Errorf("production catalog trust key %q is not canonical base64", ProductionKeyID)
	}
	return catalogpublication.NewTrustRoot(map[string]ed25519.PublicKey{
		ProductionKeyID: publicKey,
	})
}
