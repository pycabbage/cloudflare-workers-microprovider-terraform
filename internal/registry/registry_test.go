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

var allPlatforms = [][2]string{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
}

func fakeSHA(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

type relSpec struct {
	tag          string
	draft        bool
	noSums       bool
	noSig        bool
	starFormat   bool
	omitFromSums []string
	omitAssets   []string
	platforms    [][2]string
}

type serverRecorder struct {
	pages []int
	auths []string
}

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
			t.Errorf("unexpected Accept header for releases API: %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != strconv.Itoa(releasesPerPage) {
			t.Errorf("unexpected per_page parameter for releases API: %q", got)
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			t.Errorf("invalid page parameter for releases API: %q", r.URL.Query().Get("page"))
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
			t.Errorf("failed to encode releases: %v", err)
		}
	})
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("unexpected Accept header for asset download: %q", got)
		}
		content, ok := assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := w.Write(content); err != nil {
			t.Errorf("failed to write asset: %v", err)
		}
	})
	return server, rec
}

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

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("cannot parse JSON in %s: %v", path, err)
	}
}

func registryBase(cfg config) string {
	return filepath.Join(cfg.outputDir, "v1", "providers", testNamespace, testType)
}

func assertJSONKeys(t *testing.T, label string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: raw JSON is missing key %q (existing keys: %v)", label, k, slices.Sorted(maps.Keys(m)))
		}
	}
}

func rawJSONObject(t *testing.T, label string, v any) map[string]any {
	t.Helper()
	if list, ok := v.([]any); ok {
		if len(list) == 0 {
			t.Fatalf("%s: raw JSON array is empty", label)
		}
		v = list[0]
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: raw JSON is not an object: %T", label, v)
	}
	return m
}

func TestGenerateTwoVersions(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.2.0", platforms: allPlatforms},
		{tag: "v0.1.0", platforms: allPlatforms, starFormat: true},
	})
	cfg, _ := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	base := registryBase(cfg)

	var vr versionsResponse
	readJSONFile(t, filepath.Join(base, "versions"), &vr)
	if len(vr.Versions) != 2 {
		t.Fatalf("unexpected version count: got %d, want 2", len(vr.Versions))
	}
	if vr.Versions[0].Version != "0.2.0" || vr.Versions[1].Version != "0.1.0" {
		t.Errorf("unexpected version order: %+v", vr.Versions)
	}
	for _, v := range vr.Versions {
		if !slices.Equal(v.Protocols, []string{"6.0"}) {
			t.Errorf("unexpected protocols for version %s: %v", v.Version, v.Protocols)
		}
		if len(v.Platforms) != len(allPlatforms) {
			t.Fatalf("unexpected platform count for version %s: got %d, want %d", v.Version, len(v.Platforms), len(allPlatforms))
		}
		got := map[string]bool{}
		for _, p := range v.Platforms {
			got[p.OS+"/"+p.Arch] = true
		}
		for _, p := range allPlatforms {
			if !got[p[0]+"/"+p[1]] {
				t.Errorf("version %s is missing platform %s/%s", v.Version, p[0], p[1])
			}
		}
	}

	for _, ver := range []string{"0.1.0", "0.2.0"} {
		sumsName := fmt.Sprintf("%s_%s_SHA256SUMS", testProject, ver)
		urlBase := fmt.Sprintf("https://github.example/%s/releases/download/v%s", testRepo, ver)
		for _, p := range allPlatforms {
			path := filepath.Join(base, ver, "download", p[0], p[1])
			var d downloadResponse
			readJSONFile(t, path, &d)

			zipName := fmt.Sprintf("%s_%s_%s_%s.zip", testProject, ver, p[0], p[1])
			if !slices.Equal(d.Protocols, []string{"6.0"}) {
				t.Errorf("%s: unexpected protocols: %v", path, d.Protocols)
			}
			if d.OS != p[0] || d.Arch != p[1] {
				t.Errorf("%s: unexpected os/arch: %s/%s", path, d.OS, d.Arch)
			}
			if d.Filename != zipName {
				t.Errorf("%s: unexpected filename: %s", path, d.Filename)
			}
			if want := urlBase + "/" + zipName; d.DownloadURL != want {
				t.Errorf("%s: unexpected download_url: got %s, want %s", path, d.DownloadURL, want)
			}
			if want := urlBase + "/" + sumsName; d.ShasumsURL != want {
				t.Errorf("%s: unexpected shasums_url: got %s, want %s", path, d.ShasumsURL, want)
			}
			if want := urlBase + "/" + sumsName + ".sig"; d.ShasumsSignatureURL != want {
				t.Errorf("%s: unexpected shasums_signature_url: got %s, want %s", path, d.ShasumsSignatureURL, want)
			}
			if want := fakeSHA(zipName); d.Shasum != want {
				t.Errorf("%s: unexpected shasum: got %s, want %s", path, d.Shasum, want)
			}
			if len(d.SigningKeys.GPGPublicKeys) != 1 {
				t.Fatalf("%s: unexpected gpg_public_keys count: %d", path, len(d.SigningKeys.GPGPublicKeys))
			}
			key := d.SigningKeys.GPGPublicKeys[0]
			if key.KeyID != testKeyID {
				t.Errorf("%s: unexpected key_id: got %s, want %s", path, key.KeyID, testKeyID)
			}
			if key.ASCIIArmor != testArmor {
				t.Errorf("%s: ascii_armor does not match the public key file contents", path)
			}
		}
	}

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

	nojekyll, err := os.ReadFile(filepath.Join(cfg.outputDir, ".nojekyll"))
	if err != nil {
		t.Errorf(".nojekyll was not generated: %v", err)
	} else if len(nojekyll) != 0 {
		t.Errorf(".nojekyll is not an empty file")
	}
	index, err := os.ReadFile(filepath.Join(cfg.outputDir, "index.html"))
	if err != nil {
		t.Fatalf("index.html was not generated: %v", err)
	}
	wantSource := "pycabbage.github.io/pycabbage/cloudflare-workers-microprovider"
	if !strings.Contains(string(index), wantSource) {
		t.Errorf("index.html does not contain provider source %q", wantSource)
	}
}

func TestGenerateSkipsDraftAndInvalidTag(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms},
		{tag: "v0.9.0", draft: true, platforms: allPlatforms},
		{tag: "0.2.0", platforms: allPlatforms},
		{tag: "vNext", platforms: allPlatforms},
	})
	cfg, logBuf := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != 1 || vr.Versions[0].Version != "0.1.0" {
		t.Fatalf("unexpected versions (expected only 0.1.0): %+v", vr.Versions)
	}
	if _, err := os.Stat(filepath.Join(registryBase(cfg), "0.9.0")); !os.IsNotExist(err) {
		t.Errorf("a directory for draft release 0.9.0 was generated")
	}
	for _, tag := range []string{"0.2.0", "vNext"} {
		if !strings.Contains(logBuf.String(), tag) {
			t.Errorf("skip for tag %q was not logged", tag)
		}
	}
}

func TestGenerateSkipsReleaseWithoutSHA256SUMS(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.2.0", platforms: allPlatforms},
		{tag: "v0.1.0", platforms: allPlatforms, noSums: true},
		{tag: "v0.0.1", platforms: allPlatforms, noSig: true},
	})
	cfg, logBuf := newTestConfig(t, server)
	if err := generate(cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != 1 || vr.Versions[0].Version != "0.2.0" {
		t.Fatalf("unexpected versions (expected only 0.2.0): %+v", vr.Versions)
	}
	if !strings.Contains(logBuf.String(), "warning") || !strings.Contains(logBuf.String(), "SHA256SUMS") {
		t.Errorf("warning about missing SHA256SUMS was not logged: %s", logBuf.String())
	}
}

func TestGenerateFailsWhenNoValidVersions(t *testing.T) {
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, noSums: true},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), "0 valid versions found") {
		t.Fatalf("expected an error for 0 valid versions: %v", err)
	}
}

func TestGenerateFailsOnMissingShasumEntry(t *testing.T) {
	missing := fmt.Sprintf("%s_0.1.0_windows_amd64.zip", testProject)
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, omitFromSums: []string{missing}},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("expected an error for a missing SHA256SUMS entry: %v", err)
	}
}

func TestGenerateFailsOnMissingZipAsset(t *testing.T) {
	missing := fmt.Sprintf("%s_0.1.0_windows_amd64.zip", testProject)
	server, _ := buildTestServer(t, []relSpec{
		{tag: "v0.1.0", platforms: allPlatforms, omitAssets: []string{missing}},
	})
	cfg, _ := newTestConfig(t, server)
	err := generate(cfg)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("expected an error for a missing zip asset: %v", err)
	}
}

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
		t.Fatalf("generate failed: %v", err)
	}
	if !slices.Equal(rec.pages, []int{1, 2}) {
		t.Errorf("unexpected page parameters sent to releases API: got %v, want [1 2]", rec.pages)
	}
	var vr versionsResponse
	readJSONFile(t, filepath.Join(registryBase(cfg), "versions"), &vr)
	if len(vr.Versions) != releasesPerPage+1 {
		t.Errorf("unexpected version count (pages were not merged): got %d, want %d", len(vr.Versions), releasesPerPage+1)
	}
}

func TestAuthorizationHeader(t *testing.T) {
	specs := []relSpec{{tag: "v0.1.0", platforms: allPlatforms}}

	t.Run("with token", func(t *testing.T) {
		server, rec := buildTestServer(t, specs)
		cfg, _ := newTestConfig(t, server)
		cfg.token = "test-token"
		if err := generate(cfg); err != nil {
			t.Fatalf("generate failed: %v", err)
		}
		if len(rec.auths) == 0 {
			t.Fatal("mock server did not record any requests")
		}
		for i, got := range rec.auths {
			if got != "Bearer test-token" {
				t.Errorf("unexpected Authorization header for request %d: got %q, want %q", i, got, "Bearer test-token")
			}
		}
	})

	t.Run("without token", func(t *testing.T) {
		server, rec := buildTestServer(t, specs)
		cfg, _ := newTestConfig(t, server)
		if err := generate(cfg); err != nil {
			t.Fatalf("generate failed: %v", err)
		}
		if len(rec.auths) == 0 {
			t.Fatal("mock server did not record any requests")
		}
		for i, got := range rec.auths {
			if got != "" {
				t.Errorf("request %d has an Authorization header despite no token being set: %q", i, got)
			}
		}
	})
}

func TestParseSHA256SUMS(t *testing.T) {
	shaA := strings.Repeat("a1", 32)
	shaB := strings.Repeat("b2", 32)
	input := shaA + "  file_one.zip\n" +
		strings.ToUpper(shaB) + " *file_two.zip\n" +
		"\n"
	sums, err := parseSHA256SUMS([]byte(input))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(sums) != 2 {
		t.Fatalf("unexpected entry count: got %d, want 2", len(sums))
	}
	if sums["file_one.zip"] != shaA {
		t.Errorf("unexpected shasum for file_one.zip: %s", sums["file_one.zip"])
	}
	if sums["file_two.zip"] != shaB {
		t.Errorf("shasum for file_two.zip was not normalized to lowercase: %s", sums["file_two.zip"])
	}

	crlfInput := shaA + "  file_one.zip\r\n" + shaB + " *file_two.zip\r\n"
	sums, err = parseSHA256SUMS([]byte(crlfInput))
	if err != nil {
		t.Fatalf("CRLF input parse failed: %v", err)
	}
	if len(sums) != 2 {
		t.Fatalf("unexpected entry count for CRLF input: got %d, want 2", len(sums))
	}
	if sums["file_one.zip"] != shaA || sums["file_two.zip"] != shaB {
		t.Errorf("unexpected parse result for CRLF input (a trailing \\r may remain in the filename): %v", sums)
	}

	for _, bad := range []string{
		"broken line\n",
		strings.Repeat("a", 63) + "  short-hash.zip\n",
		strings.Repeat("a", 64) + "no-separator.zip\n",
	} {
		if _, err := parseSHA256SUMS([]byte(bad)); err == nil {
			t.Errorf("expected an error for invalid line %q", bad)
		}
	}
}

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
		{testProject + "_0.2.0_linux_amd64.zip", "0.1.0", "", "", false},
		{testProject + "_0.1.0_linux_amd64.tar.gz", "0.1.0", "", "", false},
		{testProject + "_0.1.0_linux_amd64_x.zip", "0.1.0", "", "", false},
		{testProject + "_0.1.0_linux.zip", "0.1.0", "", "", false},
		{"other-project_0.1.0_linux_amd64.zip", "0.1.0", "", "", false},
	}
	for _, c := range cases {
		gotOS, gotArch, gotOK := parsePlatformZip(c.name, testProject, c.version)
		if gotOS != c.wantOS || gotArch != c.wantArch || gotOK != c.wantOK {
			t.Errorf("parsePlatformZip(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.name, c.version, gotOS, gotArch, gotOK, c.wantOS, c.wantArch, c.wantOK)
		}
	}
}

func TestNormalizeKeyID(t *testing.T) {
	got, err := normalizeKeyID("abcdef0123456789")
	if err != nil || got != "ABCDEF0123456789" {
		t.Errorf("unexpected normalization of lowercase 16-char ID: got (%q, %v)", got, err)
	}
	fingerprint := "1111222233334444" + "ABCDEF0123456789"
	got, err = normalizeKeyID(fingerprint)
	if err != nil || got != "ABCDEF0123456789" {
		t.Errorf("unexpected result extracting the last 16 chars from a fingerprint: got (%q, %v)", got, err)
	}
	for _, bad := range []string{"", "xyz", "12345"} {
		if _, err := normalizeKeyID(bad); err == nil {
			t.Errorf("expected an error for invalid key ID %q", bad)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.10.0", "0.2.0", 1},
		{"1.0.0", "0.9.9", 1},
		{"1.0.0", "1.0.0-rc.1", 1},
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
			t.Errorf("compareVersions(%q, %q) sign = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := sign(compareVersions(c.b, c.a)); got != -c.want {
			t.Errorf("compareVersions(%q, %q) sign = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}
