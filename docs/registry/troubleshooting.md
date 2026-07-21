# Troubleshooting

## terraform init failures

If `terraform init` fails, check the following in order:

- Is `/.well-known/terraform.json` present in
  `pycabbage/pycabbage.github.io`? Verify with:
  `curl https://pycabbage.github.io/.well-known/terraform.json`
- Does the `providers.v1` URL in that document end with a trailing slash?
- If the user site is a branch-deployed (Jekyll) site, is `.nojekyll`
  present? Without it, `.well-known/` is not served.
- Is GitHub Pages enabled for this repository with Source set to
  "GitHub Actions"?
- Does the `github-pages` environment's protection rules allow the tag
  `v*`? If rejected, the Pages deployment fails and the registry JSON is
  never served.
- Does the `GPG_PUBLIC_KEY` secret actually match the key used for signing
  (`GPG_PRIVATE_KEY`)? A mismatch causes checksum/signature verification
  errors on the Terraform client side. Secrets cannot be read back through
  the GitHub UI or API, so verify the fingerprint locally with
  `gpg --show-keys` before registering it, rather than trying to diff it
  after the fact.

## Generator behavior: what gets skipped with a warning

The registry generator (`internal/registry`) reads every release from the
GitHub Releases API and applies these rules per release:

- **Draft releases are skipped** with a warning logged.
- **Non-semver tags are skipped** with a warning. Only tags matching
  `vX.Y.Z` (with an optional `-prerelease` suffix, e.g. `v0.1.0-rc.1`) are
  accepted; anything else (missing `v` prefix, non-numeric core, etc.) is
  skipped.
- **Releases missing `SHA256SUMS` or `SHA256SUMS.sig`** are skipped with a
  warning (both assets are required to trust and verify the release).
- **Releases with zero platform-zip assets** (no file matching
  `{project}_{version}_{os}_{arch}.zip`) are skipped with a warning, even if
  `SHA256SUMS`/`.sig` are present.

## Generator behavior: what is a hard failure

Some inconsistencies indicate a corrupted or partially-uploaded release and
are treated as unrecoverable errors that abort the entire generation run
(rather than being silently skipped), so a broken release cannot silently
propagate into the published registry site:

- A platform zip asset exists in the release but has **no corresponding
  entry in `SHA256SUMS`**.
- `SHA256SUMS` has an entry for a platform zip name but **the zip asset is
  not actually attached** to the release (e.g. a partial upload or a
  manually deleted asset).
- **Zero valid versions** remain across all releases after the
  skip rules above are applied. This aborts the whole run, since publishing
  an empty registry would break every consumer.

## -key-id flag format

`-key-id` accepts either:

- a 16-hex-character GPG key ID, or
- a 40-hex-character GPG fingerprint.

Either form is normalized to the last 16 characters, upper-cased, before
being written into the registry JSON as `signing_keys.gpg_public_keys[].key_id`.

## Key rotation caveat

The generator embeds the *current* `GPG_PUBLIC_KEY` into the `signing_keys`
field of every version's download JSON, including versions signed with a
previously rotated-out key. Rotating the key therefore breaks signature
verification for old releases that were signed with the old key, unless you
keep re-signing and re-uploading their `SHA256SUMS.sig`. In practice, treat
key rotation as a breaking change for already-published versions.

## Version ordering caveat

Versions are sorted newest-first for the `versions` endpoint. Numeric
`major.minor.patch` components are compared numerically. However,
prerelease suffixes (the part after the `-`, e.g. `rc.1` vs `rc.10`) are
compared with **simple string comparison**, not full semver precedence
rules for dot-separated numeric identifiers. This means `rc.10` sorts before
`rc.2` (because `"rc.10" < "rc.2"` as strings), which differs from proper
semver ordering. A release with no prerelease suffix is always considered
newer than one with a prerelease suffix, given equal major.minor.patch.
