# cloudflare-workers-microprovider-terraform

Cloudflare Workers の workers.dev subdomain を読み取るだけの自作マイクロ Terraform プロバイダーです。

Terraform Registry (registry.terraform.io) には公開せず、**GitHub Releases + GitHub Pages** で配布します。GitHub Pages 側に [Terraform provider registry protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol) の静的実装（JSONファイル群）を置くことで、利用側は `terraform init` を実行するだけでプロバイダーを取得できます。CLI での実行はもちろん、HCP Terraform (Terraform Cloud) 上でも動作します。

## アーキテクチャ

```
terraform init
    |
    | 1. service discovery
    v
https://pycabbage.github.io/.well-known/terraform.json
    (ユーザーサイト repo: pycabbage/pycabbage.github.io)
    |
    | 2. "providers.v1" のベースURLを取得
    v
https://pycabbage.github.io/cloudflare-workers-microprovider-terraform/v1/providers/...
    (本 repo の GitHub Pages: versions / download エンドポイントの静的JSON)
    |
    | 3. download_url を取得
    v
GitHub Releases の zip
    (terraform-provider-cloudflare-workers-microprovider_{VERSION}_{os}_{arch}.zip)
```

## 一度きりのセットアップ手順

初回リリースの前に、以下を一度だけ実施する必要があります。

1. **GPG 鍵の生成と secrets への登録**

   RSA 4096 での生成を推奨します。

   ```sh
   gpg --full-generate-key
   ```

   生成した鍵を、本リポジトリの Actions secrets に登録します。

   - `GPG_PRIVATE_KEY`: `gpg --armor --export-secret-keys <KEYID>` の出力
   - `PASSPHRASE`: 鍵生成時に設定したパスフレーズ

2. **公開鍵のコミット**

   ```sh
   gpg --armor --export <KEYID> > gpg-public-key.asc
   ```

   これをリポジトリルートにコミットします。**署名に使う鍵（`GPG_PRIVATE_KEY`）と必ず一致させてください。** 鍵をローテーションする場合は secrets と `gpg-public-key.asc` の両方を更新します。

3. **本 repo の GitHub Pages を有効化**

   Settings → Pages → Source を「GitHub Actions」に設定します。

4. **github-pages environment の deployment protection rules を調整**

   デフォルトではデプロイ元がブランチに限定されているため、タグ (`v*`) からの Pages デプロイが拒否されます。Settings → Environments → github-pages → Deployment branches and tags に `v*` のタグルールを追加してください。

5. **ユーザーサイトリポジトリに service discovery 文書を設置**

   `pycabbage/pycabbage.github.io` リポジトリに `/.well-known/terraform.json` を以下の内容で設置します。**これが無いと `terraform init` は絶対に通りません。**

   ```json
   {
     "providers.v1": "https://pycabbage.github.io/cloudflare-workers-microprovider-terraform/v1/providers/"
   }
   ```

   URL の**末尾スラッシュは必須**です。また、ブランチデプロイ（Jekyll）の場合はドット始まりのディレクトリ（`.well-known/`）が除外されるため、リポジトリルートに `.nojekyll` ファイルも必ず置いてください。

## リリース手順

タグを打って push するだけです。

```sh
git tag v0.1.0
git push origin v0.1.0
```

CI が自動で GoReleaser によるビルド・GPG 署名・GitHub Releases への公開と、レジストリサイト（GitHub Pages）の再生成・デプロイを行います。

また、workflow_dispatch で（新しいリリースを作らずに）レジストリサイトのみを再生成することもできます。

## 利用側の使い方

```hcl
terraform {
  required_providers {
    cfsubdomain = {
      source = "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
    }
  }
}

provider "cfsubdomain" {}

data "cfsubdomain_workers_subdomain" "this" {
  account_id = var.account_id
}
```

- リソース・データソースの prefix は `cfsubdomain_` です（例: `data.cfsubdomain_workers_subdomain`）。
- 追加の CLI 設定やバイナリの手動配置は不要で、`terraform init` を実行するだけで取得できます。

## 制約・注意

- リポジトリ・GitHub Releases・GitHub Pages はすべて**公開**である必要があります（プライベートだと terraform が匿名でダウンロードできません）。
- Terraform >= 1.1 が必要です（plugin protocol 6 のため）。
- レジストリ上の type 名（`cloudflare-workers-microprovider`）とリソース prefix（`cfsubdomain`）は意図的に異なる設計です。source address の type とプロバイダーの TypeName は別物であり、これは正当な構成です。
- 初回リリースの前に `gpg-public-key.asc` がリポジトリルートにコミットされている必要があります（無い場合 CI は明確なエラーで失敗します）。

## トラブルシューティング

`terraform init` が失敗する場合、以下を順に確認してください。

- [ ] `pycabbage/pycabbage.github.io` に `/.well-known/terraform.json` が設置されているか（`curl https://pycabbage.github.io/.well-known/terraform.json` で確認）
- [ ] `providers.v1` の URL の**末尾がスラッシュ**になっているか
- [ ] ユーザーサイトがブランチデプロイ（Jekyll）の場合、`.nojekyll` が置かれているか（無いと `.well-known/` が配信されない）
- [ ] 本 repo の GitHub Pages が有効化され、Source が「GitHub Actions」になっているか
- [ ] github-pages environment の保護ルールにタグ `v*` が許可されているか（拒否されると Pages デプロイが失敗し、レジストリJSONが配信されない）
- [ ] `gpg-public-key.asc` の公開鍵が、実際に署名に使われた鍵（`GPG_PRIVATE_KEY`）と一致しているか（不一致だと checksum 検証でエラーになる）
