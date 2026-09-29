package catalogcmd

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/temper-sh/temper/internal/catalog"
)

func configureExecution(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("temper execution configure", flag.ContinueOnError)
	f.SetOutput(stderr)
	lockPath := f.String("lock", "", "frozen source execution lock")
	id := f.String("preset", "", "explicit preset ID")
	window := f.Int("context", 0, "total context window in tokens")
	output := f.Int("max-output", 0, "output token allowance")
	memory := f.Int64("max-memory", 0, "Splash engine memory limit in bytes")
	out := f.String("out", "", "new execution lock path")
	dry := f.Bool("dry-run", false, "validate without writes")
	if err := f.Parse(args); err != nil {
		return 2
	}
	memorySet := false
	f.Visit(func(option *flag.Flag) { memorySet = memorySet || option.Name == "max-memory" })
	if memorySet && *memory <= 0 {
		return failed(stderr, errors.New("--max-memory must be positive"))
	}
	if f.NArg() != 0 || *lockPath == "" || *id == "" || *out == "" {
		return failed(stderr, errors.New("--lock, --preset, --context, --max-output and --out are required"))
	}
	info, err := os.Lstat(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return failed(stderr, errors.New("execution lock must be a regular file no larger than 4 MiB"))
	}
	raw, err := os.ReadFile(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	base, err := catalog.ParseLock(raw)
	if err != nil {
		return failed(stderr, err)
	}
	settings := catalog.ExecutionSettings{ContextWindowTokens: *window, MaxOutputTokens: *output, MaxMemoryBytes: *memory}
	configured, err := catalog.ConfigureExecution(base, *id, settings)
	if err != nil {
		return failed(stderr, err)
	}
	raw, err = catalog.MarshalLock(configured)
	if err != nil {
		return failed(stderr, err)
	}
	preset := configured.Records.Presets[*id]
	template := ""
	if len(preset.Patches) > 0 {
		template = preset.Patches[0]
	}
	identity, err := catalog.ContextExecutionSHA256(configured.Records, *id, template, preset.ContextWindowTokens)
	if err != nil {
		return failed(stderr, err)
	}
	changed, err := publishFile(ctx, *out, raw, *dry)
	if err != nil {
		return failed(stderr, err)
	}
	return encode(stdout, stderr, map[string]any{"schema": "temper-execution-configuration/v1", "preset": *id, "settings": settings, "context_execution_sha256": identity, "execution_digest": configured.ExecutionDigest, "path": *out, "changed": changed, "dry_run": *dry})
}
