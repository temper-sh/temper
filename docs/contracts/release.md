# Temper release artifact contract

Status: approved pre-alpha release surface, 2026-08-27.

The first public distribution is one Developer ID-signed and Apple-notarized
macOS ARM64 release asset. Homebrew and a curl installer may follow after the
alpha stabilizes; neither is required to install or run Field Kit.

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
7. verifies the checksum, signature, Gatekeeper assessment, version, archive
   shape, and embedded Field Kit catalog from a clean extraction; and
8. uses GitHub's REST API and the scoped workflow token to create a draft,
   upload both verified assets, and publish only after every prior gate passes.

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
