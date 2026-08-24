// Package catalogreader selects and verifies the catalog used by read-only
// software workflows. An authenticated active snapshot wins; an embedded,
// authenticated bootstrap is the standalone fallback. This package never
// activates or writes either one.
package catalogreader

import (
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogstore"
)

type Origin string

const (
	OriginActive    Origin = "active"
	OriginBootstrap Origin = "bootstrap"
)

type Bootstrap struct {
	CatalogData   []byte
	SignatureData []byte
}

type CapabilityValidator interface {
	ValidateCatalog(catalog.Document) error
}

type Result struct {
	Catalog catalog.Snapshot
	Origin  Origin
	KeyID   string
}

func Read(root string, trust publication.TrustRoot, bootstrap Bootstrap, capabilities CapabilityValidator) (Result, error) {
	if capabilities == nil {
		return Result{}, errors.New("catalog capability validator is required")
	}
	active, err := catalogstore.Read(root)
	if err != nil {
		return Result{}, err
	}
	origin := OriginActive
	catalogData := active.CatalogData
	signatureData := active.SignatureData
	if !active.Exists() {
		origin = OriginBootstrap
		catalogData = bootstrap.CatalogData
		signatureData = bootstrap.SignatureData
		if len(catalogData) == 0 || len(signatureData) == 0 {
			return Result{}, errors.New("no active software catalog and embedded bootstrap is unavailable")
		}
	}

	keyID, err := trust.Verify(catalogData, signatureData)
	if err != nil {
		return Result{}, fmt.Errorf("verify %s software catalog signature: %w", origin, err)
	}
	snapshot, err := catalog.ParseSnapshot(catalogData)
	if err != nil {
		return Result{}, err
	}
	if err := capabilities.ValidateCatalog(snapshot.Document); err != nil {
		return Result{}, fmt.Errorf("%s software catalog is unsupported by this binary: %w", origin, err)
	}
	return Result{Catalog: snapshot, Origin: origin, KeyID: keyID}, nil
}
