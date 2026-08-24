// Package catalogbootstrap embeds the signed software catalog available when
// no newer authenticated catalog has been activated under the Temper root.
package catalogbootstrap

import (
	_ "embed"

	"github.com/temper-sh/temper/internal/software/catalogreader"
)

//go:embed catalog.yaml
var catalogData []byte

//go:embed catalog.signature.yaml
var signatureData []byte

// Production returns independent copies of the authenticated bootstrap
// publication so no caller can mutate the process-wide embedded bytes.
func Production() catalogreader.Bootstrap {
	return catalogreader.Bootstrap{
		CatalogData:   append([]byte(nil), catalogData...),
		SignatureData: append([]byte(nil), signatureData...),
	}
}
