# One-time setup

Before the first release, complete the following steps once.

## 1. Generate a GPG key and register secrets

RSA 4096 is recommended:

```sh
gpg --full-generate-key
```

Register the generated key in this repository's Actions secrets:

- `GPG_PRIVATE_KEY`: the output of `gpg --armor --export-secret-keys <KEYID>`
- `GPG_PASSPHRASE`: the passphrase set when the key was generated
- `GPG_PUBLIC_KEY`: the output of `gpg --armor --export <KEYID>`

```sh
gpg --armor --export <KEYID> | gh secret set GPG_PUBLIC_KEY
```

`GPG_PUBLIC_KEY` must always match the key used for signing
(`GPG_PRIVATE_KEY`). If you rotate the key, update all three secrets
together. Note that a secret cannot be read back through the GitHub UI or
API, so there is no diff to inspect after rotation - double-check the
fingerprint locally with `gpg --show-keys` before registering it.

The public key is not sensitive: it is published as-is in every generated
`signing_keys.gpg_public_keys[].ascii_armor` field on the Pages site. Using
a secret here is only a convenience (no file to commit and keep in sync),
not a confidentiality measure.

## 2. Enable GitHub Pages for this repository

Settings -> Pages -> Source: set to "GitHub Actions".

## 3. Adjust deployment protection rules for the github-pages environment

By default, deployment sources are restricted to branches, which causes
Pages deployments triggered from a tag (`v*`) to be rejected. Go to
Settings -> Environments -> github-pages -> Deployment branches and tags,
and add a tag rule for `v*`.

## 4. Add the service discovery document to the user-site repository

In the `pycabbage/pycabbage.github.io` repository, create
`/.well-known/terraform.json` with the following content. Without this file,
`terraform init` cannot succeed under any circumstances:

```json
{
  "providers.v1": "https://pycabbage.github.io/cloudflare-workers-microprovider-terraform/v1/providers/"
}
```

The trailing slash on the URL is required. Also, if the user site is a
branch-deployed (Jekyll) site, dot-prefixed directories such as
`.well-known/` are excluded by default, so a `.nojekyll` file must also be
placed at the repository root.

## Visibility requirement

The repository, its GitHub Releases, and its GitHub Pages site must all be
public, since Terraform downloads the provider anonymously.
