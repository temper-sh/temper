# Temper release artifact contract

Status: direct-download release and Homebrew distribution contract, 2026-09-30.

The direct-download distribution is one Developer ID-signed and Apple-notarized
macOS ARM64 release asset. Homebrew supplies a source formula and ARM64 bottles
through this repository's tap. Field Kit continues to pin the signed ZIP;
Homebrew installation does not change its host identity.

## Version and target

A release tag is `v<SEMVER>`. The binary reports the tag without its leading
`v` through `temper version`. Release builds are fixed to:

```text
GOOS=darwin
GOARCH=arm64
CGO_ENABLED=0
```

The build uses trim paths, excludes mutable VCS metadata, and injects only the
validated version. Reusing a version for different bytes is refused.

## Asset

The release asset is:

```text
temper_<SEMVER>_darwin_arm64.zip
temper_<SEMVER>_darwin_arm64.zip.sha256
```

The ZIP has deterministic paths, order, modes, and timestamps beneath one
top-level directory of the same name without `.zip`. It contains exactly:

```text
temper
LICENSE
THIRD_PARTY_NOTICES.txt
```

`THIRD_PARTY_NOTICES.txt` is generated at packaging time from the exact module
graph linked into `cmd/temper`. Every non-standard module must contribute its
root license and notice files. These third-party bytes exist only in the
release asset; they are not copied into the 0BSD source tree.

The checksum file contains the lowercase SHA-256 identity of the exact ZIP.
Packaging is second-run clean for identical bytes and refuses a same-version
destination containing different bytes. It also executes the candidate's
read-only `version` command and refuses to package bytes reporting any other
version.

## Signing, notarization, and publication

The tag workflow runs on a native GitHub-hosted macOS ARM64 runner. It:

1. runs the complete hermetic test and vet gates;
2. builds the versioned binary;
3. imports a short-lived Developer ID Application identity from encrypted
   repository secrets;
4. signs with hardened runtime and a secure timestamp;
5. packages the signed binary and generated notices;
6. submits the ZIP with `notarytool` and waits for acceptance;
7. verifies the checksum, signature, notarization, version and archive shape
   from a clean extraction, then compiles, inspects and configures a local execution
   lock, checks configuration replay, and verifies that execution preparation, rendering
   and removal dry runs leave the installation absent; and
8. uses GitHub's REST API and the scoped workflow token to create a draft,
   upload both verified assets, and publish only after every prior gate passes.

The tagged tree must include a valid signed catalog publication and reviewed
release notes at `docs/releases/<SEMVER>.md`. The workflow uses those notes as
the release body so incompatible alpha changes are explicit.

The signing certificate, certificate password, Apple ID app password, team
identity, and temporary keychain password never enter the tree or release
asset. A missing signing/notarization credential fails closed. The temporary
keychain and certificate file are removed even after failure.

Local packaging performs no signing, notarization, upload, release creation,
model download, or live Field Kit execution. Publishing a tag and performing a
heavy Field Kit run remain separate explicit actions.

## Repository configuration

The `Release` workflow requires these encrypted GitHub Actions secrets:

| Secret | Value |
|---|---|
| `APPLE_DEVELOPER_ID_APPLICATION` | Base64-encoded Developer ID Application PKCS#12 file |
| `APPLE_DEVELOPER_ID_PASSWORD` | Password protecting that PKCS#12 file |
| `APPLE_SIGNING_IDENTITY` | Exact `Developer ID Application: ...` identity name |
| `APPLE_ID` | Apple account used for notarization |
| `APPLE_APP_PASSWORD` | App-specific password for that account |
| `APPLE_TEAM_ID` | Apple Developer team identifier |

Secret values must be entered directly in repository settings, never placed in
a command, issue, log, tracked file, or release note. A pushed strict-SemVer
tag such as `v0.1.0-alpha.1` is the only publication trigger. The workflow
creates a draft after all verification passes and exposes it only after both
assets have uploaded successfully.

## Local rehearsal

A maintainer can exercise every unsigned packaging boundary in a disposable
directory:

```sh
go run ./cmd/temper-release build \
  --version 0.1.0-alpha.1 \
  --output /tmp/temper-release/build/temper
go run ./cmd/temper-release package \
  --version 0.1.0-alpha.1 \
  --binary /tmp/temper-release/build/temper \
  --output /tmp/temper-release/dist
(cd /tmp/temper-release/dist && \
  shasum -a 256 -c temper_0.1.0-alpha.1_darwin_arm64.zip.sha256)
```

Running the two Go commands again must report `state=unchanged`. A different
binary or checksum at an existing version is a hard error. The example version
is illustrative; a rehearsal does not create or reserve a tag.

## Homebrew formula and bottles

`Formula/temper.rb` makes the existing repository a tap; no separate tap
repository or credential is needed:

```sh
brew tap temper-sh/temper https://github.com/temper-sh/temper
brew trust --formula temper-sh/temper/temper
brew install temper-sh/temper/temper
```

Homebrew 7 requires explicit formula trust; older versions omit that command.
The formula is macOS ARM64 only. Its SHA-256 pins the tagged source archive,
and its build invokes the existing `temper-release build` and `package`
commands. The keg contains the CLI, Temper's license and the generated linked
dependency notices. Go is a build dependency, not a bottle runtime dependency.
There is no Homebrew service or post-install setup hook. Installing, upgrading
or removing the formula does not configure models or manage a Temper root.
Stop active layouts before upgrading or removing their CLI: a managed layout
retains the exact launcher path from activation.

These are ordinary Homebrew builds and checksummed bottles, independently built
from the release's source. They do not carry the direct ZIP's Developer ID
signature or notarization. The signed ZIP remains available for callers that
require that distribution identity.

The `Homebrew` workflow runs after the signed release has been published. It
can also prepare or publish a bottle for an existing release through
`workflow_dispatch`; its `publish` input defaults to false. It:

1. checks out the current default branch as the maintained tap;
2. verifies the named release exists, is public and contains the direct ZIP;
3. checksums the source archive and updates the formula with Homebrew's
   `bump-formula-pr --write-only`, refusing a version downgrade;
4. builds the formula with `--build-bottle`, runs `brew test`, and creates a
   genuine Homebrew bottle with `brew bottle --json`;
5. merges the generated bottle block without committing, removes the build's
   keg, pours the bottle, then reruns the formula test and strict audit;
6. retains the verified formula, bottle and Homebrew bottle JSON as a workflow
   artifact; and
7. when publication was requested, uploads the bottle and JSON to the existing
   release before committing only the updated formula to the default branch.

The native `macos-15` ARM64 runner emits an `arm64_sequoia` bottle. Homebrew
chooses compatible bottles on newer macOS versions; other eligible versions
use the source build. A locally prepared bottle retains its actual platform tag.
Local bottles are rehearsal artifacts: their Homebrew receipts can contain
workstation paths. Publish bottles from CI. The initial maintained formula
supports source installation; its bottle block is added only after the CI
archive is uploaded, so it never advertises an unavailable download.
The formula test verifies the version, reads an empty configuration without
creating its root, and checks that dependency notices were installed. It needs
no model, network request, Metal device or launchd operation.

Publication uses the workflow's `contents: write` token. The default branch
must permit that workflow to commit the formula. The release tag and existing
ZIP/checksum are never rewritten. A published bottle and its JSON are reused
on a repeated run, preserving their exact bytes. Existing assets are compared
before upload; different bytes at the same name refuse publication. The formula
advances only after both assets are available. A concurrent branch update
refuses the push rather than force-pushing; rerunning uses the existing bottle
and applies its formula to the current branch.

For the initial release or a failed formula update, invoke the same workflow:

```sh
gh workflow run homebrew.yml --repo temper-sh/temper \
  -f tag=v0.1.0-alpha.11 -F publish=false
```

Review its `homebrew-v0.1.0-alpha.11` artifact and checks, then repeat with
`-F publish=true` to publish. A partial upload with no bottle JSON is recovered
using the retained workflow artifact; upload its missing bytes unchanged.
Do not delete or overwrite an existing bottle to make a retry succeed. A
deliberate packaging change to already published bytes requires Homebrew's
bottle rebuild mechanism and a reviewed formula update.
