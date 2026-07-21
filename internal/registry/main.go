// Command registry は GitHub Releases の情報から Terraform provider registry
// protocol 準拠の静的レジストリサイト (GitHub Pages 用) を生成する。
//
// 使い方:
//
//	go run ./internal/registry \
//	  -namespace pycabbage \
//	  -type cloudflare-workers-microprovider \
//	  -public-key-file gpg-public-key.asc \
//	  -key-id ABCDEF0123456789 \
//	  -output _site
//
// 参照する環境変数:
//
//	GITHUB_REPOSITORY  リリースを取得するリポジトリ (owner/repo 形式、必須)
//	GITHUB_API_URL     GitHub API のベースURL (未設定時は https://api.github.com)
//	GITHUB_TOKEN       設定されていれば Authorization: Bearer として送信 (任意)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		namespace     string
		providerType  string
		publicKeyFile string
		keyID         string
		output        string
	)
	flag.StringVar(&namespace, "namespace", "", "プロバイダーの namespace (例: pycabbage)")
	flag.StringVar(&providerType, "type", "", "プロバイダーの type (例: cloudflare-workers-microprovider)")
	flag.StringVar(&publicKeyFile, "public-key-file", "", "ASCII-armored GPG 公開鍵ファイルのパス")
	flag.StringVar(&keyID, "key-id", "", "GPG long key ID (fingerprint 末尾16桁の大文字hex)")
	flag.StringVar(&output, "output", "", "レジストリサイトの出力先ディレクトリ")
	flag.Parse()

	cfg, err := newConfig(namespace, providerType, publicKeyFile, keyID, output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
	if err := generate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		os.Exit(1)
	}
}

// newConfig はフラグ値と環境変数から config を組み立て、入力を検証する。
func newConfig(namespace, providerType, publicKeyFile, keyID, output string) (config, error) {
	switch {
	case namespace == "":
		return config{}, fmt.Errorf("-namespace は必須です")
	case providerType == "":
		return config{}, fmt.Errorf("-type は必須です")
	case publicKeyFile == "":
		return config{}, fmt.Errorf("-public-key-file は必須です")
	case keyID == "":
		return config{}, fmt.Errorf("-key-id は必須です")
	case output == "":
		return config{}, fmt.Errorf("-output は必須です")
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return config{}, fmt.Errorf("環境変数 GITHUB_REPOSITORY が設定されていません (owner/repo 形式で指定してください)")
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return config{}, fmt.Errorf("環境変数 GITHUB_REPOSITORY %q が owner/repo 形式ではありません", repo)
	}

	apiURL := os.Getenv("GITHUB_API_URL")
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}

	return config{
		namespace:     namespace,
		providerType:  providerType,
		publicKeyFile: publicKeyFile,
		keyID:         keyID,
		outputDir:     output,
		repo:          repo,
		apiURL:        apiURL,
		token:         os.Getenv("GITHUB_TOKEN"),
	}, nil
}
