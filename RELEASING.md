# Releasing epub-tools

Releases are built and published by GitHub Actions
(`.github/workflows/release.yml`) when a version tag is pushed. Nothing is
built on a laptop, and no secrets beyond the default `GITHUB_TOKEN` are needed.

## Cut a release

1. **Make sure `main` is green.** The latest CI run on `main` must pass,
   including the `release-dry-run` job, which builds every archive without
   publishing.

2. **Pick the version** ([semver](https://semver.org)):
   - patch `v0.1.1` — bug fixes only
   - minor `v0.2.0` — new flags or behaviour, nothing existing breaks
   - major `v1.0.0` — a change that breaks existing usage (renamed flags,
     different default output names)
   - Tags with a suffix (`v0.2.0-rc.1`) are published as pre-releases and are
     not picked up by `install.sh` as "latest".

3. **Tag `main` and push the tag:**

   ```sh
   git checkout main && git pull --ff-only
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

   The workflow refuses tags that do not point at a commit on `main`.

4. **Watch the run:** `gh run watch` or the repository's Actions tab. It runs
   the tests and govulncheck, then GoReleaser builds and uploads:
   - `epub-tools_<version>_{darwin,linux}_{amd64,arm64}.tar.gz`
   - `epub-tools_<version>_windows_{amd64,arm64}.zip`
   - `checksums.txt`
   - signed build provenance attestations for all of the above

   The changelog in the release notes is generated from commit messages
   (`feat:` → Features, `fix:` → Fixes).

5. **Check it as a user would**, in a fresh terminal:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/gkwang/epub-tools/main/install.sh | sh
   epub-tools version          # shows the new version
   gh attestation verify ~/Downloads/epub-tools_0.1.0_darwin_arm64.tar.gz --repo gkwang/epub-tools
   ```

## Try the release build locally

```sh
go run github.com/goreleaser/goreleaser/v2@v2.19.0 release --snapshot --clean
./dist/epub-tools_darwin_arm64_v8.0/epub-tools version
```

`install.sh` can be pointed at a local copy of a release for testing:

```sh
EPUB_TOOLS_BASE_URL=http://127.0.0.1:8000 EPUB_TOOLS_VERSION=9.9.9 \
EPUB_TOOLS_INSTALL_DIR=/tmp/et sh install.sh
```

where the server serves `<tag>/<archive>` and `<tag>/checksums.txt`.

## If a release is bad

- **Workflow failed before publishing:** fix on `main`, delete the tag
  (`git push --delete origin v0.1.0 && git tag -d v0.1.0`), and tag again.
- **Published but broken:** do not reuse the version number — users may
  already have it. Release a fixed patch version (`v0.1.1`). If the broken
  release is harmful, mark it as a pre-release or delete it on the Releases
  page so `install.sh` stops picking it up as latest.

## Maintenance

- GitHub Actions and the GoReleaser/govulncheck versions are pinned
  (actions by commit SHA). Update them deliberately, checking release notes.
- The Go version comes from `go.mod`. Bump it when govulncheck reports a
  standard-library fix, as was done for go1.26.10.
