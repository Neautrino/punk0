# Publishing & Release Guide

This document outlines the step-by-step instructions for preparing, testing, and publishing a new release of `punk0`.

## 1. Prerequisites

Before cutting a new release, ensure you have:

- Git with push and tag permissions for `Neautrino/punk0`.
- Go (version 1.25+).
- Bun for bundling frontend assets.
- A clean working tree with all tests passing.

## 2. Pre-Release Verification

Run the test suite and verify asset bundling:

```bash
# 1. Run unit and integration tests
make test

# 2. Build local binary and verify sanity
make build
./punk0 -version
```

### Write the release notes

Release notes live in [punk0-web](https://github.com/Neautrino/punk0-web), one file per release: `src/data/changelog/v<version>.md` (front matter `version` and `date`, then `### Added` / `### Changed` / `### Removed` / `### Fixed` groups; see its README). Write the file before tagging, and push it to punk0-web once the release is published: the push rebuilds the site's changelog page. The same notes go into the GitHub release (section 4).

## 3. Release Methods
### Method 1: Automated Release via Makefile (Recommended)

The project includes an automated `make publish` target in the [Makefile](Makefile) that:

1. Updates the [VERSION](VERSION) file.
1. Bundles web assets and cross-compiles binaries into `dist/`.
1. Commits `VERSION`.
1. Creates an annotated Git tag `v<version>`.

#### Step 1: Run publish command

Specify the version without a leading `v`:

```bash
make publish 0.2.0
```

#### Step 2: Push commit and tags to GitHub

```bash
git push origin main --tags
```

### Method 2: Manual Release via Git

```bash
# 1. Update VERSION file
echo "0.2.0" > VERSION

# 2. Bundle frontend assets and compile release binaries
make dist

# 3. Commit version bump
git add VERSION
git commit -m "Release v0.2.0"

# 4. Tag the release
git tag -fa v0.2.0 -m "Release v0.2.0"

# 5. Push to GitHub
git push origin main
git push origin v0.2.0
```

### Method 3: GitHub Actions Workflow Dispatch

1. Navigate to Actions -> Release on GitHub: `https://github.com/Neautrino/punk0/actions/workflows/release.yml`
1. Click Run workflow.
1. Enter the version tag (e.g. `v0.2.0`).
1. Trigger the workflow.

## 4. Post-Release Verification

1. Verify GitHub Actions workflow completion on the Actions tab.
1. Confirm artifacts on the [Releases](https://github.com/Neautrino/punk0/releases) page (cross-platform binaries and `checksums.txt`). The self-updater requires this file and verifies the selected binary against it before execution.
1. Replace the generated release notes with the release's notes from punk0-web (paste from a file, not from terminal output: GitHub keeps hard line breaks):
  ```bash
  gh release edit v<version> -R Neautrino/punk0 --notes-file notes.md
  ```
1. Push the release notes file to punk0-web and check https://punk0.neautrino.dev/changelog/.
1. Verify the installer script:
  ```bash
  curl -fsSL https://raw.githubusercontent.com/Neautrino/punk0/main/install.sh | sh
  ```
1. Verify self-update functionality:
  ```bash
  punk0 -update
  ```
