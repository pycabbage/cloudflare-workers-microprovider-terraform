package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	testRepo      = "pycabbage/cloudflare-workers-microprovider-terraform"
	testNamespace = "pycabbage"
	testType      = "cloudflare-workers-microprovider"
	testProject   = "terraform-provider-" + testType
	testKeyID     = "ABCDEF0123456789"
	testArmor     = "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQINBFtestdata\n-----END PGP PUBLIC KEY BLOCK-----\n"
)

// allPlatforms はリリース対象の5プラットフォーム。
var allPlatforms = [][2]string{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
}

// fakeSHA はテスト用の決定的な SHA256 値 (ファイル名自体のハッシュ) を返す。
func fakeSHA(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// relSpec はモックする GitHub リリース1件分の仕様。
type relSpec struct {
	tag          string
	draft        bool
	noSums       bool     // SHA256SUMS アセットを付けない
	noSig        bool     // SHA256SUMS.sig アセットを付けない
	starFormat   bool     // SHA256SUMS の行を "<hash> *<name>" 形式にする
	omitFromSums []string // SHA256SUMS から除外する zip 名 (壊れたリリースの再現用)
	omitAssets   []string // アセットとして付けない zip 名 (SHA256SUMS のエントリは残す)
	platforms    [][2]string
}

// serverRecorder はモックサーバが受け取ったリクエストの記録。
type serverRecorder struct {
	pages []int    // releases API に来たリクエストの page パラメータ (受信順)
	auths []string // 全リクエストの Authorization ヘッダ (受信順、無ければ空文字)
}

// buildTestServer は GitHub API (releases 一覧 + アセットダウンロード) を
// モックする httptest.Server と、受信リクエストの記録を組み立てる。
// releases 一覧は本物の API と同様に per_page 件ずつページネーションして返す。
func buildTestServer(t *testing.T, specs []relSpec) (*httptest.Server, *serverRecorder) {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	rec := &serverRecorder{}

	assets := map[string][]byte{}
	var releases []map[string]any
	for i, sp := range specs {
		version := strings.TrimPrefix(sp.tag, "v")
		newAsset := func(name string) map[string]any {
			return map[string]any{
				"name":                 name,
				"url":                  fmt.Sprintf("%s/assets/%d/%s", server.URL, i, name),
				"browser_download_url": fmt.Sprintf("https://github.example/%s/releases/download/%s/%s", testRepo, sp.tag, name),
			}
		}
		var assetList []map[string]any
		var sumLines []string
		for _, p := range sp.platforms {
			name := fmt.Sprintf("%s_%s_%s_%s.zip", testProject, version, p[0], p[1])
			if !slices.Contains(sp.omitAssets, name) {
				assetList = append(assetList, newAsset(name))
			}
			if slices.Contains(sp.omitFromSums, name) {
				continue
			}
			sep := "  "
			if sp.starFormat {
				sep = " *"
			}
			sumLines = append(sumLines, fakeSHA(name)+sep+name)
		}
		sumsName := fmt.Sprintf("%s_%s_SHA256SUMS", testProject, version)
		if !sp.noSums {
			assetList = append(assetList, newAsset(sumsName))
			assets[fmt.Sprintf("/assets/%d/%s", i, sumsName)] = []byte(strings.Join(sumLines, "\n") + "\n")
		}
		if !sp.noSig {
			assetList = append(assetList, newAsset(sumsName+".sig"))
		}
		releases = append(releases, map[string]any{
			"tag_name": sp.tag,
			"draft":    sp.draft,
			"assets":   assetList,
		})
	}

	mux.HandleFunc("/repos/"+testRepo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("releases API の Accept ヘッダが不正: %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != strconv.Itoa(releasesPerPage) {
			t.Errorf("releases API の per_page パラメータが想定外: %q", got)
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			t.Errorf("releases API の page パラメータが不正: %q", r.URL.Query().Get("page"))
			page = 1
		}
		rec.pages = append(rec.pages, page)
		start := min((page-1)*releasesPerPage, len(releases))
		end := min(start+releasesPerPage, len(releases))
		pageReleases := releases[start:end]
		if pageReleases == nil {
			pageReleases = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pageReleases); err != nil {
			t.Errorf("releases のエンコードに失敗: %v", err)
		}
	})
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("asset ダウンロードの Accept ヘッダが不正: %q", got)
		}
		content, ok := assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(content); err != nil {
			t.Errorf("asset の書き込みに失敗: %v", err)
		}
	})
	return server, rec
}

// newTestConfig はテスト用の config (一時ディレクトリ・公開鍵ファイル込み) を作る。
func newTestConfig(t *testing.T, server *httptest.Server) (config, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "gpg-public-key.asc")
	if err := os.WriteFile(keyFile, []byte(testArmor), 0o644); err != nil {
		t.Fatal(err)
	}
	logBuf := &bytes.Buffer{}
	return config{
		namespace:     testNamespace,
		providerType:  testType,
		publicKeyFile: keyFile,
		keyID:         testKeyID,
		outputDir:     filepath.Join(dir, "_site"),
		repo:          testRepo,
		apiURL:        server.URL,
		httpClient:    server.Client(),
		logW:          logBuf,
	}, logBuf
}

// readJSONFile は生成されたファイルを読み込んで JSON としてパースする。
func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読み込めません: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s の JSON をパースできません: %v", path, err)
	}
}

// registryBase はレジストリ JSON の出力先ベースディレクトリ。
func registryBase(cfg config) string {
	return filepath.Join(cfg.outputDir, "v1", "providers", testNamespace, testType)
}

// assertJSONKeys は生 JSON のオブジェクトに契約上のキー名が全て存在することを検証する。
// 本番コードと同じ struct タグで round-trip すると json タグの typo を検出できないため、
// キー名を文字列リテラルでロックする。
func assertJSONKeys(t *testing.T, label string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s の生 JSON にキー %q がありません (存在するキー: %v)", label, k, slices.Sorted(maps.Keys(m)))
		}
	}
}

// rawJSONObject は生 JSON の値 (map / 配列の先頭要素) をオブジェクトとして取り出す。
func rawJSONObject(t *testing.T, label string, v any) map[string]any {
	t.Helper()
	if list, ok := v.([]any); ok {
		if len(list) == 0 {
			t.Fatalf("%s の生 JSON 配列が空です", label)
		}
		v = list[0]
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s の生 JSON がオブジェクトではありません: %T", label, v)
	}
	return m
}

// TestGenerateTwoVersions は正常系: 2バージョン×5プラットフォームのリリースから
// versions と全 download JSON が契約通りに生成されることを検証する。
func TestGenerateTwoVersions(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.2.0", platforms: allPlatforms},
		// 片方は sha256sum のバイナリモード ("*" 付き) 形式でパースを検証する
		{tag: "v0.1.0", platforms: allPlatforms, starFormat: true},
	})
	cfg, _ := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate が失敗しました: %v", err)
	}
	base := registryBase(cfg)

	// versions エンドポイント
	var vr versionsResponse
	readJSONFile(t, filepath.Join(base, "versions"), &vr)
	if len(vr.Versions) != 2 {
		t.Fatalf("バージョン数が想定外: got %d, want 2", len(vr.Versions))
	}
	if vr.Versions[0].Version != "0.2.0" || vr.Versions[1].Version != "0.1.0" {
		t.Errorf("バージョンの並びが想定外: %+v", vr.Versions)
	}
	for _, v := range vr.Versions {
		if !slices.Equal(v.Protocols, []string{"6.0"}) {
			t.Errorf("バージョン %s の protocols が想定外: %v", v.Version, v.Protocols)
		}
		if len(v.Platforms) != len(allPlatforms) {
			t.Fatalf("バージョン %s のプラットフォーム数が想定外: got %d, want %d", v.Version, len(v.Platforms), len(allPlatforms))
		}
		got := map[string]bool{}
		for _, p := range v.Platforms {
			got[p.OS+"/"+p.Arch] = true
		}
		for _, p := range allPlatforms {
			if !got[p[0]+"/"+p[1]] {
				t.Errorf("バージョン %s に platform %s/%s がありません", v.Version, p[0], p[1])
			}
		}
	}

	// download エンドポイント (全バージョン × 全プラットフォーム)
	for _, ver := range []string{"0.1.0", "0.2.0"} {
		sumsName := fmt.Sprintf("%s_%s_SHA256SUMS", testProject, ver)
		urlBase := fmt.Sprintf("https://github.example/%s/releases/download/v%s", testRepo, ver)
		for _, p := range allPlatforms {
			path := filepath.Join(base, ver, "download", p[0], p[1])
			var d downloadResponse
			readJSONFile(t, path, &d)

			zipName := fmt.Sprintf("%s_%s_%s_%s.zip", testProject, ver, p[0], p[1])
			if !slices.Equal(d.Protocols, []string{"6.0"}) {
				t.Errorf("%s: protocols が想定外: %v", path, d.Protocols)
			}
			if d.OS != p[0] || d.Arch != p[1] {
				t.Errorf("%s: os/arch が想定外: %s/%s", path, d.OS, d.Arch)
			}
			if d.Filename != zipName {
				t.Errorf("%s: filename が想定外: %s", path, d.Filename)
			}
			if want := urlBase + "/" + zipName; d.DownloadURL != want {
				t.Errorf("%s: download_url が想定外: got %s, want %s", path, d.DownloadURL, want)
			}
			if want := urlBase + "/" + sumsName; d.ShasumsURL != want {
				t.Errorf("%s: shasums_url が想定外: got %s, want %s", path, d.ShasumsURL, want)
			}
			if want := urlBase + "/" + sumsName + ".sig"; d.ShasumsSignatureURL != want {
				t.Errorf("%s: shasums_signature_url が想定外: got %s, want %s", path, d.ShasumsSignatureURL, want)
			}
			if want := fakeSHA(zipName); d.Shasum != want {
				t.Errorf("%s: shasum が想定外: got %s, want %s", path, d.Shasum, want)
			}
			if len(d.SigningKeys.GPGPublicKeys) != 1 {
				t.Fatalf("%s: gpg_public_keys の数が想定外: %d", path, len(d.SigningKeys.GPGPublicKeys))
			}
			key := d.SigningKeys.GPGPublicKeys[0]
			if key.KeyID != testKeyID {
				t.Errorf("%s: key_id が想定外: got %s, want %s", path, key.KeyID, testKeyID)
			}
			if key.ASCIIArmor != testArmor {
				t.Errorf("%s: ascii_armor が公開鍵ファイルの内容と一致しません", path)
			}
		}
	}

	// 生 JSON のキー名検証: 本番と同じ struct へ unmarshal する検証だけでは
	// json タグの typo (生成側・検証側で同じ誤ったキーになる) を検出できないため、
	// 契約上の全キー名が生の JSON に存在することを明示的にロックする。
	var rawVersions map[string]any
	readJSONFile(t, filepath.Join(base, "versions"), &rawVersions)
	assertJSONKeys(t, "versions", rawVersions, "versions")
	rawVersion := rawJSONObject(t, "versions.versions", rawVersions["versions"])
	assertJSONKeys(t, "versions.versions[0]", rawVersion, "version", "protocols", "platforms")
	rawPlatform := rawJSONObject(t, "versions.versions[0].platforms", rawVersion["platforms"])
	assertJSONKeys(t, "versions.versions[0].platforms[0]", rawPlatform, "os", "arch")

	var rawDownload map[string]any
	readJSONFile(t, filepath.Join(base, "0.1.0", "download", "linux", "amd64"), &rawDownload)
	assertJSONKeys(t, "download", rawDownload,
		"protocols", "os", "arch", "filename", "download_url",
		"shasums_url", "shasums_signature_url", "shasum", "signing_keys")
	rawSigning := rawJSONObject(t, "download.signing_keys", rawDownload["signing_keys"])
	assertJSONKeys(t, "download.signing_keys", rawSigning, "gpg_public_keys")
	rawKey := rawJSONObject(t, "download.signing_keys.gpg_public_keys", rawSigning["gpg_public_keys"])
	assertJSONKeys(t, "download.signing_keys.gpg_public_keys[0]", rawKey, "key_id", "ascii_armor")

	// .nojekyll と index.html
	nojekyll, err := os.ReadFile(filepath.Join(cfg.outputDir, ".nojekyll"))
	if err != nil {
		t.Errorf(".nojekyll が生成されていません: %v", err)
	} else if len(nojekyll) != 0 {
		t.Errorf(".nojekyll が空ファイルではありません")
	}
	index, err := os.ReadFile(filepath.Join(cfg.outputDir, "index.html"))
	if err != nil {
		t.Fatalf("index.html が生成されていません: %v", err)
	}
	wantSource := "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
	if !strings.Contains(string(index), wantSource) {
		t.Errorf("index.html に provider source %q が含まれていません", wantSource)
	}
}

// TestGenerateSkipsDraftAndInvalidTag は draft リリースと
// タグ形式不正のリリースが除外されることを検証する。
func TestGenerateSkipsDraftAndInvalidTag(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms},
		{tag: "v0.9.0", draft: true, platforms: allPlatforms},
		{tag: "0.2.0", platforms: allPlatforms}, // v 無し
		{tag: "vNext", platforms: allPlatforms}, // 数値でない
	})
	cfg, logBuf := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate が失敗しました: %v", err)
	}

	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != 1 || vr.Versions[0].Version != "0.1.0" {
		t.Fatalf("versions が想定外 (0.1.0 のみのはず): %+v", vr.Versions)
	}
	if _, err := os.Stat(filepath.Join(registryBase(cfg), "0.9.0")); !os.IsNotExist(err) {
		t.Errorf("draft リリース 0.9.0 のディレクトリが生成されています")
	}
	for _, tag := range []string{"0.2.0", "vNext"} {
		if !strings.Contains(logBuf.String(), tag) {
			t.Errorf("タグ %q のスキップがログに出力されていません", tag)
		}
	}
}

// TestGenerateSkipsReleaseWithoutSHA256SUMS は SHA256SUMS または .sig を
// 欠くリリースが警告付きでスキップされることを検証する。
func TestGenerateSkipsReleaseWithoutSHA256SUMS(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.2.0", platforms: allPlatforms},
		{tag: "v0.1.0", platforms: allPlatforms, noSums: true},
		{tag: "v0.0.1", platforms: allPlatforms, noSig: true},
	})
	cfg, logBuf := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate が失敗しました: %v", err)
	}

	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != 1 || vr.Versions[0].Version != "0.2.0" {
		t.Fatalf("versions が想定外 (0.2.0 のみのはず): %+v", vr.Versions)
	}
	if !strings.Contains(logBuf.String(), "警告") || !strings.Contains(logBuf.String(), "SHA256SUMS") {
		t.Errorf("SHA256SUMS 欠落の警告がログに出力されていません: %s", logBuf.String())
	}
}

// TestGenerateFailsWhenNoValidVersions は有効なバージョンが0件のとき
// エラーになることを検証する。
func TestGenerateFailsWhenNoValidVersions(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, noSums: true},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), "有効なバージョンが0件") {
		t.Fatalf("有効バージョン0件のエラーになりません: %v", err)
	}
}

// TestGenerateFailsOnMissingShasumEntry は zip に対応する SHA256SUMS の
// エントリが無い (壊れたリリース) 場合にエラーになることを検証する。
func TestGenerateFailsOnMissingShasumEntry(t *testing.T) {
	missing := fmt.Sprintf("%s_0.1.0_windows_amd64.zip", testProject)
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, omitFromSums: []string{missing}},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("SHA256SUMS エントリ欠落がエラーになりません: %v", err)
	}
}

// TestGenerateFailsOnMissingZipAsset は SHA256SUMS にエントリがあるのに
// 対応する zip アセットがリリースに存在しない (部分アップロード失敗などの
// 壊れたリリース) 場合に、黙って欠落させずエラーになることを検証する。
func TestGenerateFailsOnMissingZipAsset(t *testing.T) {
	missing := fmt.Sprintf("%s_0.1.0_windows_amd64.zip", testProject)
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, omitAssets: []string{missing}},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("zip アセット欠落がエラーになりません: %v", err)
	}
}

// TestFetchReleasesPagination は 1ページ (releasesPerPage 件) を超えるリリースが
// ページネーションで全件取得・結合されることを検証する。
func TestFetchReleasesPagination(t *testing.T) {
	specs := make([]relSpec, 0, releasesPerPage+1)
	for i := 1; i <= releasesPerPage+1; i++ {
		specs = append(specs, relSpec{
			tag:       fmt.Sprintf("v0.0.%d", i),
			platforms: [][2]string{{"linux", "amd64"}},
		})
	}
	server, rec := buildTestServer(t, specs)
	cfg, _ := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate が失敗しました: %v", err)
	}
	// 1ページ目が満杯 (releasesPerPage 件) のため 2ページ目まで取得される。
	if !slices.Equal(rec.pages, []int{1, 2}) {
		t.Errorf("releases API へのリクエストの page パラメータが想定外: got %v, want [1 2]", rec.pages)
	}
	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != releasesPerPage+1 {
		t.Errorf("バージョン数が想定外 (2ページ分が結合されていません): got %d, want %d", len(vr.Versions), releasesPerPage+1)
	}
}

// TestAuthorizationHeader は GITHUB_TOKEN が設定されているとき全リクエストに
// Authorization: Bearer ヘッダが付き、未設定のとき付かないことを検証する。
func TestAuthorizationHeader(t *testing.T) {
	specs := []relSpec{{tag: "v0.1.0", platforms: allPlatforms}}

	t.Run("トークンあり", func(t *testing.T) {
		server, rec := buildTestServer(t, specs)
		cfg, _ := newTestConfig(t, server)
		cfg.token = "test-token"
		if err := generate(cfg); err != nil {
			t.Fatalf("generate が失敗しました: %v", err)
		}
		if len(rec.auths) == 0 {
			t.Fatal("モックサーバがリクエストを記録していません")
		}
		for i, got := range rec.auths {
			if got != "Bearer test-token" {
				t.Errorf("リクエスト %d の Authorization ヘッダが想定外: got %q, want %q", i, got, "Bearer test-token")
			}
		}
	})

	t.Run("トークンなし", func(t *testing.T) {
		server, rec := buildTestServer(t, specs)
		cfg, _ := newTestConfig(t, server)
		if err := generate(cfg); err != nil {
			t.Fatalf("generate が失敗しました: %v", err)
		}
		if len(rec.auths) == 0 {
			t.Fatal("モックサーバがリクエストを記録していません")
		}
		for i, got := range rec.auths {
			if got != "" {
				t.Errorf("リクエスト %d にトークン未設定なのに Authorization ヘッダが付いています: %q", i, got)
			}
		}
	})
}

// TestParseSHA256SUMS はチェックサムファイルのパース (2空白形式・"*"付き形式・
// 大文字hexの正規化・不正行のエラー) を検証する。
func TestParseSHA256SUMS(t *testing.T) {
	shaA := strings.Repeat("a1", 32)
	shaB := strings.Repeat("b2", 32)
	input := shaA + "  file_one.zip\n" +
		strings.ToUpper(shaB) + " *file_two.zip\n" +
		"\n"
	sums, err := parseSHA256SUMS([]byte(input))
	if err != nil {
		t.Fatalf("パースに失敗しました: %v", err)
	}
	if len(sums) != 2 {
		t.Fatalf("エントリ数が想定外: got %d, want 2", len(sums))
	}
	if sums["file_one.zip"] != shaA {
		t.Errorf("file_one.zip の shasum が想定外: %s", sums["file_one.zip"])
	}
	if sums["file_two.zip"] != shaB {
		t.Errorf("file_two.zip の shasum が小文字に正規化されていません: %s", sums["file_two.zip"])
	}

	// CRLF (Windows 改行) の入力でもファイル名に \r が残らないこと
	crlfInput := shaA + "  file_one.zip\r\n" + shaB + " *file_two.zip\r\n"
	sums, err = parseSHA256SUMS([]byte(crlfInput))
	if err != nil {
		t.Fatalf("CRLF 入力のパースに失敗しました: %v", err)
	}
	if len(sums) != 2 {
		t.Fatalf("CRLF 入力のエントリ数が想定外: got %d, want 2", len(sums))
	}
	if sums["file_one.zip"] != shaA || sums["file_two.zip"] != shaB {
		t.Errorf("CRLF 入力のパース結果が想定外 (\\r がファイル名に残っている可能性): %v", sums)
	}

	for _, bad := range []string{
		"broken line\n",
		strings.Repeat("a", 63) + "  short-hash.zip\n", // hex が63桁
		strings.Repeat("a", 64) + "no-separator.zip\n", // 区切りの空白なし
	} {
		if _, err := parseSHA256SUMS([]byte(bad)); err == nil {
			t.Errorf("不正な行 %q がエラーになりません", bad)
		}
	}
}

// TestParsePlatformZip は zip ファイル名からの os/arch 抽出を検証する。
func TestParsePlatformZip(t *testing.T) {
	cases := []struct {
		name     string
		version  string
		wantOS   string
		wantArch string
		wantOK   bool
	}{
		{testProject + "_0.1.0_linux_amd64.zip", "0.1.0", "linux", "amd64", true},
		{testProject + "_0.1.0_windows_amd64.zip", "0.1.0", "windows", "amd64", true},
		{testProject + "_0.1.0-rc.1_darwin_arm64.zip", "0.1.0-rc.1", "darwin", "arm64", true},
		{testProject + "_0.1.0_SHA256SUMS", "0.1.0", "", "", false},
		{testProject + "_0.1.0_SHA256SUMS.sig", "0.1.0", "", "", false},
		{testProject + "_0.2.0_linux_amd64.zip", "0.1.0", "", "", false},    // バージョン不一致
		{testProject + "_0.1.0_linux_amd64.tar.gz", "0.1.0", "", "", false}, // 拡張子違い
		{testProject + "_0.1.0_linux_amd64_x.zip", "0.1.0", "", "", false},  // 区切りが多い
		{testProject + "_0.1.0_linux.zip", "0.1.0", "", "", false},          // arch 無し
		{"other-project_0.1.0_linux_amd64.zip", "0.1.0", "", "", false},     // プレフィックス不一致
	}
	for _, c := range cases {
		gotOS, gotArch, gotOK := parsePlatformZip(c.name, testProject, c.version)
		if gotOS != c.wantOS || gotArch != c.wantArch || gotOK != c.wantOK {
			t.Errorf("parsePlatformZip(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.name, c.version, gotOS, gotArch, gotOK, c.wantOS, c.wantArch, c.wantOK)
		}
	}
}

// TestNormalizeKeyID は key ID の正規化 (大文字化・fingerprint の末尾16桁抽出) を検証する。
func TestNormalizeKeyID(t *testing.T) {
	got, err := normalizeKeyID("abcdef0123456789")
	if err != nil || got != "ABCDEF0123456789" {
		t.Errorf("小文字16桁の正規化が想定外: got (%q, %v)", got, err)
	}
	fingerprint := "1111222233334444" + "ABCDEF0123456789"
	got, err = normalizeKeyID(fingerprint)
	if err != nil || got != "ABCDEF0123456789" {
		t.Errorf("fingerprint からの末尾16桁抽出が想定外: got (%q, %v)", got, err)
	}
	for _, bad := range []string{"", "xyz", "12345"} {
		if _, err := normalizeKeyID(bad); err == nil {
			t.Errorf("不正な key ID %q がエラーになりません", bad)
		}
	}
}

// TestCompareVersions はバージョン比較 (数値比較・プレリリース) を検証する。
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // 符号のみ比較
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.10.0", "0.2.0", 1}, // 数値として比較される
		{"1.0.0", "0.9.9", 1},
		{"1.0.0", "1.0.0-rc.1", 1}, // プレリリース無しの方が新しい
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		default:
			return 0
		}
	}
	for _, c := range cases {
		if got := sign(compareVersions(c.a, c.b)); got != c.want {
			t.Errorf("compareVersions(%q, %q) の符号 = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := sign(compareVersions(c.b, c.a)); got != -c.want {
			t.Errorf("compareVersions(%q, %q) の符号 = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}
