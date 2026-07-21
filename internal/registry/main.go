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
	flag.StringVar(&namespace, "namespace", "", "provider namespace (e.g. pycabbage)")
	flag.StringVar(&providerType, "type", "", "provider type (e.g. cloudflare-workers-microprovider)")
	flag.StringVar(&publicKeyFile, "public-key-file", "", "path to the ASCII-armored GPG public key file")
	flag.StringVar(&keyID, "key-id", "", "GPG long key ID (last 16 hex chars of the fingerprint, uppercase)")
	flag.StringVar(&output, "output", "", "output directory for the registry site")
	flag.Parse()

	cfg, err := newConfig(namespace, providerType, publicKeyFile, keyID, output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := generate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newConfig(namespace, providerType, publicKeyFile, keyID, output string) (config, error) {
	switch {
	case namespace == "":
		return config{}, fmt.Errorf("-namespace is required")
	case providerType == "":
		return config{}, fmt.Errorf("-type is required")
	case publicKeyFile == "":
		return config{}, fmt.Errorf("-public-key-file is required")
	case keyID == "":
		return config{}, fmt.Errorf("-key-id is required")
	case output == "":
		return config{}, fmt.Errorf("-output is required")
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return config{}, fmt.Errorf("environment variable GITHUB_REPOSITORY is not set (must be owner/repo format)")
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return config{}, fmt.Errorf("environment variable GITHUB_REPOSITORY %q is not in owner/repo format", repo)
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
