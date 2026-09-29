package adapter_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
)

func TestNewRegistryRejectsDuplicateKeys(t *testing.T) {
	homebrew := descriptor("homebrew", "system-package", "shared", software.Target{OS: "darwin", Arch: "arm64"})
	_, err := adapter.NewRegistry(homebrew, homebrew)
	if err == nil || !strings.Contains(err.Error(), "registered more than once") {
		t.Fatalf("NewRegistry() error = %v, want duplicate refusal", err)
	}
}

func descriptor(id, method, effectModel string, target software.Target) adapter.Descriptor {
	return adapter.Descriptor{
		ID:          id,
		Method:      method,
		Protocol:    adapter.Protocol,
		EffectModel: effectModel,
		Targets:     []software.Target{target},
	}
}
