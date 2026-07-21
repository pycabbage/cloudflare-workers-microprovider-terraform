# Release procedure

## Cutting a release

Tag and push:

```sh
git tag v0.1.0
git push origin v0.1.0
```

CI then automatically builds and signs the binaries with GoReleaser,
publishes them to GitHub Releases, and regenerates and deploys the registry
site (GitHub Pages).

`workflow_dispatch` can also be used to regenerate only the registry site
(without creating a new release).

## What the workflow jobs do

The `Release` workflow (`.github/workflows/release.yml`) is triggered by
pushing a `v*` tag or by manual `workflow_dispatch`, and has two jobs:

- `goreleaser`: runs only on the tag-push event (`if:
  github.event_name == 'push'`). It checks out full history (GoReleaser
  needs tags and history), imports the GPG signing key, and runs
  `goreleaser release --clean`, which builds all platform binaries, zips
  them, computes `SHA256SUMS`, signs it, and uploads everything to the
  GitHub Release. It has `contents: write` permission (needed to create the
  release and upload assets).

- `registry`: depends on `goreleaser` and runs if that job succeeded or was
  skipped, but not if it was cancelled:
  `if: ${{ !cancelled() && (needs.goreleaser.result == 'success' ||
  needs.goreleaser.result == 'skipped') }}`.
  This allows `workflow_dispatch` runs (where `goreleaser` is skipped
  because of its `if` condition) to still regenerate and redeploy the
  registry site from whatever releases already exist. It has `pages: write`
  and `id-token: write` permissions, deploys to the `github-pages`
  environment, and uses concurrency group `github-pages` with
  `cancel-in-progress: false` so that Pages deployments never overlap but an
  in-progress deployment is never aborted mid-flight.

Top-level workflow permissions are set to `{}` (no permissions at all), and
each job grants itself only the permissions it actually needs, following the
principle of least privilege.

## Composite action internals

The `registry` job delegates the actual site generation to the composite
action at `.github/actions/generate-registry/action.yml`, which:

1. Checks that the public key file (input `public-key-file`, normally
   `gpg-public-key.asc`) exists. If it is missing, the action fails with a
   clear error telling you to commit it (see `docs/registry/setup.md`).
2. Extracts the GPG key ID from that public key file by running
   `gpg --show-keys --with-colons <file>` and reading the `fpr` record: the
   10th colon-separated field of the `fpr` line is the 40-hex-character
   fingerprint, and the last 16 characters of it are used as the key ID
   passed to the generator.
3. Runs `go run ./internal/registry` with the flags:
   - `-namespace` (e.g. `pycabbage`)
   - `-type` (e.g. `cloudflare-workers-microprovider`)
   - `-public-key-file`
   - `-key-id` (the fingerprint suffix extracted in step 2)
   - `-output` (the output directory for the generated site)

   and the environment variables `GITHUB_REPOSITORY`, `GITHUB_API_URL`, and
   `GITHUB_TOKEN`, which the generator reads to know which repository's
   releases to read and how to authenticate against the GitHub API. See
   `docs/registry/troubleshooting.md` for the generator's exact
   inclusion/exclusion behavior.

## Running the generator locally

`internal/registry` can also be run directly:

```sh
go run ./internal/registry \
  -namespace pycabbage \
  -type cloudflare-workers-microprovider \
  -public-key-file gpg-public-key.asc \
  -key-id ABCDEF0123456789 \
  -output _site
```

It reads these environment variables:

- `GITHUB_REPOSITORY` - repository to read releases from, `owner/repo` format
  (required)
- `GITHUB_API_URL` - GitHub API base URL (defaults to
  `https://api.github.com`)
- `GITHUB_TOKEN` - if set, sent as `Authorization: Bearer` (optional)

## GoReleaser build details

`.goreleaser.yml` drives the `goreleaser` job:

- `before.hooks` runs `go mod tidy` first, to confirm `go.mod`/`go.sum` are
  consistent before building.
- `CGO_ENABLED=0` is set explicitly for every build, since GoReleaser does
  not disable CGO automatically.
- `ldflags` is just `-s -w` (strip symbols/debug info); there is no `-X`
  version-injection flag because `main.go` does not have a version
  variable to inject into.
- The GPG signing step signs the `SHA256SUMS` checksum file using
  `--local-user {{ .Env.GPG_FINGERPRINT }}`. `GPG_FINGERPRINT` is set in the
  workflow from the `fingerprint` output of the `crazy-max/ghaction-import-gpg`
  step that imports `secrets.GPG_PRIVATE_KEY`.
- `project_name` is set explicitly to
  `terraform-provider-cloudflare-workers-microprovider` because it differs
  from the repository name; see `docs/registry/architecture.md` for the
  full naming contract.
