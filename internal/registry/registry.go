package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
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

const releasesPerPage = 100

var protocolVersions = []string{"6.0"}

type config struct {
	namespace     string
	providerType  string
	publicKeyFile string
	keyID         string
	outputDir     string

	repo   string
	apiURL string
	token  string

	logW       io.Writer
	httpClient *http.Client
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Draft   bool          `json:"draft"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

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

type providerVersion struct {
	version             string
	shasumsURL          string
	shasumsSignatureURL string
	platforms           []platformArtifact
}

type platformArtifact struct {
	osName      string
	arch        string
	filename    string
	downloadURL string
	shasum      string
}

func generate(cfg config) error {
	if cfg.logW == nil {
		cfg.logW = os.Stderr
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	armorBytes, err := os.ReadFile(cfg.publicKeyFile)
	if err != nil {
		return fmt.Errorf("cannot read GPG public key file (check that the GPG_PUBLIC_KEY secret is set): %w", err)
	}
	armor := string(armorBytes)
	if !strings.Contains(armor, "BEGIN PGP PUBLIC KEY BLOCK") {
		return fmt.Errorf("%s is not an ASCII-armored GPG public key", cfg.publicKeyFile)
	}

	keyID, err := normalizeKeyID(cfg.keyID)
	if err != nil {
		return err
	}

	projectName := "terraform-provider-" + cfg.providerType

	client := &apiClient{
		apiURL: cfg.apiURL,
		repo:   cfg.repo,
		token:  cfg.token,
		http:   cfg.httpClient,
	}

	fmt.Fprintf(cfg.logW, "fetching release list for GitHub repository %s...\n", cfg.repo)
	releases, err := client.fetchReleases()
	if err != nil {
		return err
	}
	fmt.Fprintf(cfg.logW, "fetched %d releases\n", len(releases))

	versions, err := collectVersions(client, releases, projectName, cfg.logW)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("0 valid versions found: no release of %s has a complete set of %s_{version}_{os}_{arch}.zip plus SHA256SUMS / SHA256SUMS.sig", cfg.repo, projectName)
	}

	slices.SortFunc(versions, func(a, b providerVersion) int {
		return compareVersions(b.version, a.version)
	})

	fileCount, err := writeRegistry(cfg, keyID, armor, versions)
	if err != nil {
		return err
	}

	fmt.Fprintf(cfg.logW, "done: generated %d versions / %d files in %s\n", len(versions), fileCount, cfg.outputDir)
	return nil
}

type apiClient struct {
	apiURL string
	repo   string
	token  string
	http   *http.Client
}

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
		return nil, fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body for GET %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s failed: %s: %s", url, resp.Status, truncateForError(body))
	}
	return body, nil
}

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
			return nil, fmt.Errorf("cannot parse release list JSON (GET %s): %w", url, err)
		}
		all = append(all, releases...)
		if len(releases) < releasesPerPage {
			break
		}
	}
	return all, nil
}

func (c *apiClient) downloadAsset(asset githubAsset) ([]byte, error) {
	return c.get(asset.URL, "application/octet-stream")
}

func truncateForError(body []byte) string {
	const max = 2048
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "...(truncated)"
	}
	return s
}

var tagRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

func versionFromTag(tag string) (string, bool) {
	if !tagRe.MatchString(tag) {
		return "", false
	}
	return strings.TrimPrefix(tag, "v"), true
}

func collectVersions(client *apiClient, releases []githubRelease, projectName string, logW io.Writer) ([]providerVersion, error) {
	var versions []providerVersion
	for _, rel := range releases {
		if rel.Draft {
			fmt.Fprintf(logW, "skipping: %q is a draft release\n", rel.TagName)
			continue
		}
		version, ok := versionFromTag(rel.TagName)
		if !ok {
			fmt.Fprintf(logW, "warning: skipping tag %q because it is not a semantic version (vX.Y.Z)\n", rel.TagName)
			continue
		}
		pv, err := buildVersion(client, rel, projectName, version, logW)
		if err != nil {
			return nil, err
		}
		if pv == nil {
			continue
		}
		fmt.Fprintf(logW, "release %s: registering %d platforms\n", rel.TagName, len(pv.platforms))
		versions = append(versions, *pv)
	}
	return versions, nil
}

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
		fmt.Fprintf(logW, "warning: skipping release %s because it is missing %s or %s\n", rel.TagName, sumsName, sigName)
		return nil, nil
	}

	sumsData, err := client.downloadAsset(*sumsAsset)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s for release %s: %w", sumsName, rel.TagName, err)
	}
	sums, err := parseSHA256SUMS(sumsData)
	if err != nil {
		return nil, fmt.Errorf("release %s %s: %w", rel.TagName, sumsName, err)
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
			return nil, fmt.Errorf("release %s is broken: %s has no entry for %s", rel.TagName, sumsName, asset.Name)
		}
		pv.platforms = append(pv.platforms, platformArtifact{
			osName:      osName,
			arch:        arch,
			filename:    asset.Name,
			downloadURL: asset.BrowserDownloadURL,
			shasum:      sha,
		})
	}
	for _, name := range slices.Sorted(maps.Keys(sums)) {
		if _, _, ok := parsePlatformZip(name, projectName, version); !ok {
			continue
		}
		if !zipAssets[name] {
			return nil, fmt.Errorf("release %s is broken: %s has an entry for %s but no matching zip asset exists", rel.TagName, sumsName, name)
		}
	}
	if len(pv.platforms) == 0 {
		fmt.Fprintf(logW, "warning: skipping release %s because it has no per-platform zip (%s_%s_{os}_{arch}.zip)\n", rel.TagName, projectName, version)
		return nil, nil
	}

	slices.SortFunc(pv.platforms, func(a, b platformArtifact) int {
		if c := strings.Compare(a.osName, b.osName); c != 0 {
			return c
		}
		return strings.Compare(a.arch, b.arch)
	})
	return pv, nil
}

var shaLineRe = regexp.MustCompile(`^([0-9a-fA-F]{64})[ \t]+\*?(.+)$`)

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
			return nil, fmt.Errorf("cannot parse line %d of SHA256SUMS: %q", lineNo, line)
		}
		sums[m[2]] = strings.ToLower(m[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read SHA256SUMS: %w", err)
	}
	return sums, nil
}

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

var keyIDRe = regexp.MustCompile(`^[0-9A-Fa-f]{16,64}$`)

func normalizeKeyID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if !keyIDRe.MatchString(s) {
		return "", fmt.Errorf("invalid key ID %q (specify a hex string of 16 or more characters)", raw)
	}
	s = strings.ToUpper(s)
	return s[len(s)-16:], nil
}

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
		return 1
	case bpre == "":
		return -1
	default:
		return strings.Compare(apre, bpre)
	}
}

func writeRegistry(cfg config, keyID, armor string, versions []providerVersion) (int, error) {
	if err := os.MkdirAll(cfg.outputDir, 0o755); err != nil {
		return 0, err
	}
	fileCount := 0
	base := filepath.Join(cfg.outputDir, "v1", "providers", cfg.namespace, cfg.providerType)

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

	if err := os.WriteFile(filepath.Join(cfg.outputDir, ".nojekyll"), nil, 0o644); err != nil {
		return 0, err
	}
	fileCount++

	return fileCount, nil
}

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
