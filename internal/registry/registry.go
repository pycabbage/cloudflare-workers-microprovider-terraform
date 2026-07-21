package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// releasesPerPage は GitHub Releases API の1ページあたりの取得件数。
const releasesPerPage = 100

// protocolVersions は terraform-plugin-framework が実装する
// Terraform plugin protocol のバージョン (v6)。
var protocolVersions = []string{"6.0"}

// config は静的レジストリ生成に必要な入力のまとまり。
type config struct {
	namespace     string // プロバイダーの namespace (例: pycabbage)
	providerType  string // プロバイダーの type (例: cloudflare-workers-microprovider)
	publicKeyFile string // ASCII-armored GPG 公開鍵ファイルのパス
	keyID         string // GPG key ID (16桁以上のhex。long key ID へ正規化される)
	outputDir     string // レジストリサイトの出力先ディレクトリ

	repo   string // owner/repo 形式のリポジトリ
	apiURL string // GitHub API のベースURL
	token  string // GITHUB_TOKEN (空なら未認証でアクセス)

	// 以下はテスト用の差し替えポイント。nil なら generate 内で既定値が入る。
	logW       io.Writer    // 進捗・警告の出力先 (既定: os.Stderr)
	httpClient *http.Client // GitHub API へのアクセスに使う HTTP クライアント
}

// githubRelease は GitHub Releases API のレスポンスのうち必要なフィールドのみ。
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Draft   bool          `json:"draft"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset はリリースアセット。URL は API 経由のダウンロード用エンドポイント、
// BrowserDownloadURL は公開ダウンロードURL。
type githubAsset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// versionsResponse は versions エンドポイントの JSON。
type versionsResponse struct {
	Versions []versionEntry `json:"versions"`
}

type versionEntry struct {
	Version   string          `json:"version"`
	Protocols []string        `json:"protocols"`
	Platforms []platformEntry `json:"platforms"`
}

type platformEntry struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// downloadResponse は download エンドポイントの JSON (全フィールド必須)。
type downloadResponse struct {
	Protocols           []string    `json:"protocols"`
	OS                  string      `json:"os"`
	Arch                string      `json:"arch"`
	Filename            string      `json:"filename"`
	DownloadURL         string      `json:"download_url"`
	ShasumsURL          string      `json:"shasums_url"`
	ShasumsSignatureURL string      `json:"shasums_signature_url"`
	Shasum              string      `json:"shasum"`
	SigningKeys         signingKeys `json:"signing_keys"`
}

type signingKeys struct {
	GPGPublicKeys []gpgPublicKey `json:"gpg_public_keys"`
}

type gpgPublicKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
}

// providerVersion は1リリース分の集計結果。
type providerVersion struct {
	version             string
	shasumsURL          string // SHA256SUMS アセットの browser_download_url
	shasumsSignatureURL string // SHA256SUMS.sig アセットの browser_download_url
	platforms           []platformArtifact
}

// platformArtifact はプラットフォーム別 zip 1つ分の情報。
type platformArtifact struct {
	osName      string
	arch        string
	filename    string
	downloadURL string // zip アセットの browser_download_url
	shasum      string // SHA256SUMS からパースした 64桁hex小文字
}

// generate がこのコマンドの本体。GitHub Releases を読み取り、
// Terraform provider registry protocol の静的ファイル一式を出力する。
func generate(cfg config) error {
	if cfg.logW == nil {
		cfg.logW = os.Stderr
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	// GPG 公開鍵はリポジトリにコミットされている前提。無ければ明確に失敗させる。
	armorBytes, err := os.ReadFile(cfg.publicKeyFile)
	if err != nil {
		return fmt.Errorf("GPG公開鍵ファイルを読み込めません (リポジトリルートに gpg-public-key.asc がコミットされているか確認してください): %w", err)
	}
	armor := string(armorBytes)
	if !strings.Contains(armor, "BEGIN PGP PUBLIC KEY BLOCK") {
		return fmt.Errorf("%s は ASCII-armored の GPG 公開鍵ではありません", cfg.publicKeyFile)
	}

	keyID, err := normalizeKeyID(cfg.keyID)
	if err != nil {
		return err
	}

	// GoReleaser の project_name。リリースアセット名の共通プレフィックス。
	projectName := "terraform-provider-" + cfg.providerType

	client := &apiClient{
		apiURL: cfg.apiURL,
		repo:   cfg.repo,
		token:  cfg.token,
		http:   cfg.httpClient,
	}

	fmt.Fprintf(cfg.logW, "GitHub リポジトリ %s のリリース一覧を取得しています...\n", cfg.repo)
	releases, err := client.fetchReleases()
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.logW, "%d 件のリリースを取得しました\n", len(releases))

	versions, err := collectVersions(client, releases, projectName, cfg.logW)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("有効なバージョンが0件です: %s のリリースに %s_{version}_{os}_{arch}.zip と SHA256SUMS / SHA256SUMS.sig が揃ったものが見つかりません", cfg.repo, projectName)
	}

	// 新しいバージョンが先頭に来るように降順で並べる (出力の決定性のため)。
	slices.SortFunc(versions, func(a, b providerVersion) int {
		return compareVersions(b.version, a.version)
	})

	fileCount, err := writeRegistry(cfg, keyID, armor, versions)
	if err != nil {
		return err
	}

	fmt.Fprintf(cfg.logW, "完了: %d バージョン / %d ファイルを %s に生成しました\n", len(versions), fileCount, cfg.outputDir)
	return nil
}

// apiClient は GitHub REST API への薄いクライアント。
type apiClient struct {
	apiURL string
	repo   string
	token  string
	http   *http.Client
}

// get は url へ GET し、2xx 以外はステータスとレスポンスbody付きのエラーにする。
func (c *apiClient) get(url, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s に失敗しました: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("GET %s のレスポンス読み込みに失敗しました: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s が失敗しました: %s: %s", url, resp.Status, truncateForError(body))
	}
	return body, nil
}

// fetchReleases はリリース一覧をページネーションで全件取得する。
// 取得件数が per_page 未満になったページで終了する。
func (c *apiClient) fetchReleases() ([]githubRelease, error) {
	base := strings.TrimSuffix(c.apiURL, "/")
	var all []githubRelease
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d&page=%d", base, c.repo, releasesPerPage, page)
		body, err := c.get(url, "application/vnd.github+json")
		if err != nil {
			return nil, err
		}
		var releases []githubRelease
		if err := json.Unmarshal(body, &releases); err != nil {
			return nil, fmt.Errorf("リリース一覧のJSONをパースできません (GET %s): %w", url, err)
		}
		all = append(all, releases...)
		if len(releases) < releasesPerPage {
			break
		}
	}
	return all, nil
}

// downloadAsset はアセットを API URL 経由 (Accept: application/octet-stream) で
// ダウンロードする。リダイレクトは http.Client のデフォルト動作で追従される。
func (c *apiClient) downloadAsset(asset githubAsset) ([]byte, error) {
	return c.get(asset.URL, "application/octet-stream")
}

// truncateForError はエラーメッセージへ含めるレスポンスbodyを適当な長さに丸める。
func truncateForError(body []byte) string {
	const max = 2048
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "...(省略)"
	}
	return s
}

// tagRe はリリースタグとして受け付けるセマンティックバージョン形式
// (vX.Y.Z、-プレリリースサフィックス許容)。
var tagRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// versionFromTag はタグを検証し、先頭の v を除いたバージョンを返す。
func versionFromTag(tag string) (string, bool) {
	if !tagRe.MatchString(tag) {
		return "", false
	}
	return strings.TrimPrefix(tag, "v"), true
}

// collectVersions はリリース一覧から有効なプロバイダーバージョンを組み立てる。
// draft・タグ形式不正・アセット不足のリリースはスキップし、
// SHA256SUMS と zip の不整合 (壊れたリリース) はエラーにする。
func collectVersions(client *apiClient, releases []githubRelease, projectName string, logW io.Writer) ([]providerVersion, error) {
	var versions []providerVersion
	for _, rel := range releases {
		if rel.Draft {
			fmt.Fprintf(logW, "スキップ: %q は draft リリースです\n", rel.TagName)
			continue
		}
		version, ok := versionFromTag(rel.TagName)
		if !ok {
			fmt.Fprintf(logW, "警告: タグ %q はセマンティックバージョン形式 (vX.Y.Z) ではないためスキップします\n", rel.TagName)
			continue
		}
		pv, err := buildVersion(client, rel, projectName, version, logW)
		if err != nil {
			return nil, err
		}
		if pv == nil {
			continue // 警告付きスキップ済み
		}
		fmt.Fprintf(logW, "リリース %s: %d プラットフォームを登録します\n", rel.TagName, len(pv.platforms))
		versions = append(versions, *pv)
	}
	return versions, nil
}

// buildVersion は1リリース分のアセットを検証・集計する。
// スキップすべきリリースでは (nil, nil) を返す。
func buildVersion(client *apiClient, rel githubRelease, projectName, version string, logW io.Writer) (*providerVersion, error) {
	sumsName := fmt.Sprintf("%s_%s_SHA256SUMS", projectName, version)
	sigName := sumsName + ".sig"

	var sumsAsset, sigAsset *githubAsset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case sumsName:
			sumsAsset = &rel.Assets[i]
		case sigName:
			sigAsset = &rel.Assets[i]
		}
	}
	if sumsAsset == nil || sigAsset == nil {
		fmt.Fprintf(logW, "警告: リリース %s に %s または %s が無いためスキップします\n", rel.TagName, sumsName, sigName)
		return nil, nil
	}

	sumsData, err := client.downloadAsset(*sumsAsset)
	if err != nil {
		return nil, fmt.Errorf("リリース %s の %s のダウンロードに失敗しました: %w", rel.TagName, sumsName, err)
	}
	sums, err := parseSHA256SUMS(sumsData)
	if err != nil {
		return nil, fmt.Errorf("リリース %s の %s: %w", rel.TagName, sumsName, err)
	}

	pv := &providerVersion{
		version:             version,
		shasumsURL:          sumsAsset.BrowserDownloadURL,
		shasumsSignatureURL: sigAsset.BrowserDownloadURL,
	}
	zipAssets := make(map[string]bool, len(rel.Assets))
	for _, asset := range rel.Assets {
		osName, arch, ok := parsePlatformZip(asset.Name, projectName, version)
		if !ok {
			continue
		}
		zipAssets[asset.Name] = true
		sha, ok := sums[asset.Name]
		if !ok {
			// zip があるのにチェックサムが無いリリースは壊れているため、
			// スキップではなく全体を失敗させる。
			return nil, fmt.Errorf("リリース %s は壊れています: %s に %s のエントリがありません", rel.TagName, sumsName, asset.Name)
		}
		pv.platforms = append(pv.platforms, platformArtifact{
			osName:      osName,
			arch:        arch,
			filename:    asset.Name,
			downloadURL: asset.BrowserDownloadURL,
			shasum:      sha,
		})
	}
	// 逆方向の不整合も検査する: SHA256SUMS にエントリがあるのに対応する zip
	// アセットがリリースに存在しない場合 (部分アップロード失敗や手動削除)、
	// そのプラットフォームだけ黙って欠落してしまうため、壊れたリリースとして
	// 全体を失敗させる。
	for _, name := range slices.Sorted(maps.Keys(sums)) {
		if _, _, ok := parsePlatformZip(name, projectName, version); !ok {
			continue
		}
		if !zipAssets[name] {
			return nil, fmt.Errorf("リリース %s は壊れています: %s に %s のエントリがありますが zip アセットが存在しません", rel.TagName, sumsName, name)
		}
	}
	if len(pv.platforms) == 0 {
		fmt.Fprintf(logW, "警告: リリース %s にプラットフォーム別 zip (%s_%s_{os}_{arch}.zip) が無いためスキップします\n", rel.TagName, projectName, version)
		return nil, nil
	}

	// 出力の決定性のため os, arch で整列する。
	slices.SortFunc(pv.platforms, func(a, b platformArtifact) int {
		if c := strings.Compare(a.osName, b.osName); c != 0 {
			return c
		}
		return strings.Compare(a.arch, b.arch)
	})
	return pv, nil
}

// shaLineRe は SHA256SUMS の1行。
// 形式: <64桁hex><空白><空白または*><ファイル名> ("*" は sha256sum のバイナリモード)。
var shaLineRe = regexp.MustCompile(`^([0-9a-fA-F]{64})[ \t]+\*?(.+)$`)

// parseSHA256SUMS は SHA256SUMS の内容を filename → sha256 (小文字hex) の map にする。
// 空行は無視し、形式に合わない行はエラーにする。
func parseSHA256SUMS(data []byte) (map[string]string, error) {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := shaLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("SHA256SUMS の %d 行目をパースできません: %q", lineNo, line)
		}
		sums[m[2]] = strings.ToLower(m[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SHA256SUMS の読み込みに失敗しました: %w", err)
	}
	return sums, nil
}

// parsePlatformZip はアセット名が {projectName}_{version}_{os}_{arch}.zip に
// 一致する場合に os / arch を取り出す。
func parsePlatformZip(assetName, projectName, version string) (osName, arch string, ok bool) {
	prefix := projectName + "_" + version + "_"
	if !strings.HasPrefix(assetName, prefix) || !strings.HasSuffix(assetName, ".zip") {
		return "", "", false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(assetName, prefix), ".zip")
	parts := strings.Split(rest, "_")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// keyIDRe は key ID / fingerprint として受け付けるhex文字列。
var keyIDRe = regexp.MustCompile(`^[0-9A-Fa-f]{16,64}$`)

// normalizeKeyID は与えられた key ID または fingerprint を GPG long key ID
// (fingerprint 末尾16桁・大文字hex) へ正規化する。
func normalizeKeyID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if !keyIDRe.MatchString(s) {
		return "", fmt.Errorf("key ID %q が不正です (16桁以上のhex文字列を指定してください)", raw)
	}
	s = strings.ToUpper(s)
	return s[len(s)-16:], nil
}

// parseVersion は "X.Y.Z(-pre)" を数値3つとプレリリース文字列に分解する。
// 形式は versionFromTag で検証済みの前提。
func parseVersion(v string) (nums [3]int, pre string) {
	core, preRaw, _ := strings.Cut(v, "-")
	pre = preRaw
	for i, part := range strings.SplitN(core, ".", 3) {
		if n, err := strconv.Atoi(part); err == nil {
			nums[i] = n
		}
	}
	return nums, pre
}

// compareVersions はセマンティックバージョン2つを比較する (a<b: 負, a==b: 0, a>b: 正)。
// プレリリース同士の優先順位は簡易的に文字列比較で決める。
func compareVersions(a, b string) int {
	an, apre := parseVersion(a)
	bn, bpre := parseVersion(b)
	for i := range an {
		if an[i] != bn[i] {
			return cmp.Compare(an[i], bn[i])
		}
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1 // プレリリース無しの方が新しい
	case bpre == "":
		return -1
	default:
		return strings.Compare(apre, bpre)
	}
}

// writeRegistry はレジストリの全ファイルを出力し、書き出したファイル数を返す。
func writeRegistry(cfg config, keyID, armor string, versions []providerVersion) (int, error) {
	if err := os.MkdirAll(cfg.outputDir, 0o755); err != nil {
		return 0, err
	}
	fileCount := 0
	base := filepath.Join(cfg.outputDir, "v1", "providers", cfg.namespace, cfg.providerType)

	// versions エンドポイント (全バージョン一覧、拡張子なしファイル)
	var vr versionsResponse
	for _, v := range versions {
		entry := versionEntry{
			Version:   v.version,
			Protocols: protocolVersions,
		}
		for _, p := range v.platforms {
			entry.Platforms = append(entry.Platforms, platformEntry{OS: p.osName, Arch: p.arch})
		}
		vr.Versions = append(vr.Versions, entry)
	}
	if err := writeJSON(filepath.Join(base, "versions"), vr); err != nil {
		return 0, err
	}
	fileCount++

	// download エンドポイント (バージョン × プラットフォーム)
	signing := signingKeys{
		GPGPublicKeys: []gpgPublicKey{{KeyID: keyID, ASCIIArmor: armor}},
	}
	for _, v := range versions {
		for _, p := range v.platforms {
			d := downloadResponse{
				Protocols:           protocolVersions,
				OS:                  p.osName,
				Arch:                p.arch,
				Filename:            p.filename,
				DownloadURL:         p.downloadURL,
				ShasumsURL:          v.shasumsURL,
				ShasumsSignatureURL: v.shasumsSignatureURL,
				Shasum:              p.shasum,
				SigningKeys:         signing,
			}
			if err := writeJSON(filepath.Join(base, v.version, "download", p.osName, p.arch), d); err != nil {
				return 0, err
			}
			fileCount++
		}
	}

	// GitHub Pages で拡張子なしファイルをそのまま配信させるための .nojekyll (空ファイル)
	if err := os.WriteFile(filepath.Join(cfg.outputDir, ".nojekyll"), nil, 0o644); err != nil {
		return 0, err
	}
	fileCount++

	// トップページ。GitHub Pages のユーザーサイトドメイン (owner.github.io) を
	// レジストリの hostname とする前提で provider source を組み立てる。
	owner, _, _ := strings.Cut(cfg.repo, "/")
	source := fmt.Sprintf("%s.github.io/%s/%s", owner, cfg.namespace, cfg.providerType)
	indexHTML := renderIndexHTML(source, cfg.namespace, cfg.providerType, versions)
	if err := os.WriteFile(filepath.Join(cfg.outputDir, "index.html"), []byte(indexHTML), 0o644); err != nil {
		return 0, err
	}
	fileCount++

	return fileCount, nil
}

// writeJSON は v を整形した JSON として path に書き出す (親ディレクトリも作成する)。
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// renderIndexHTML は出力ルートに置く簡素なトップページを生成する。
func renderIndexHTML(source, namespace, providerType string, versions []providerVersion) string {
	versionsPath := fmt.Sprintf("v1/providers/%s/%s/versions", namespace, providerType)
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(source))
	b.WriteString("</head>\n<body>\n")
	b.WriteString("<h1>Terraform Provider 静的レジストリ</h1>\n")
	b.WriteString("<p>このサイトは Terraform provider registry protocol に準拠した静的レジストリです。</p>\n")
	fmt.Fprintf(&b, "<p>provider source: <code>%s</code></p>\n", html.EscapeString(source))
	fmt.Fprintf(&b, "<h2>利用可能なバージョン (<a href=\"%s\">versions エンドポイント</a>)</h2>\n<ul>\n", html.EscapeString(versionsPath))
	for _, v := range versions {
		fmt.Fprintf(&b, "<li>%s</li>\n", html.EscapeString(v.version))
	}
	b.WriteString("</ul>\n</body>\n</html>\n")
	return b.String()
}
