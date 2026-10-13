package catalogtrust_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

const productionTrustVector = "temper production catalog trust root test vector v1\n"

var productionTrustVectorSignature = []byte(`schema: temper-signature/v1
key_id: temper-catalog-2026-01
algorithm: ed25519
signature: NySP72SuSHhF1ThBuHaQrCnUFxX00s68c6YbVW5yOla7aahwIpLeD7ndQXQYPNLxwOjEAQn4tFftqBZgr8z7Ag==
`)

func TestProductionTrustRootVerifiesPinnedKeyVector(t *testing.T) {
	trust, err := catalogtrust.Production()
	if err != nil {
		t.Fatal(err)
	}
	keyID, err := trust.Verify([]byte(productionTrustVector), productionTrustVectorSignature)
	if err != nil {
		t.Fatal(err)
	}
	if keyID != catalogtrust.ProductionKeyID {
		t.Fatalf("verified key id = %q, want %q", keyID, catalogtrust.ProductionKeyID)
	}
}
