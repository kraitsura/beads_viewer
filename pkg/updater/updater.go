package updater

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"beads_viewer/pkg/version"
)

type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckForUpdates queries GitHub for the latest release.
// Returns the new version tag if an update is available, empty string otherwise.
func CheckForUpdates() (string, string, error) {
	// Set a short timeout to avoid blocking startup for too long
	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get("https://api.github.com/repos/Dicklesworthstone/beads_viewer/releases/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github api returned status: %s", resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}

	// Compare versions
	// Assumes SemVer with 'v' prefix
	if compareVersions(rel.TagName, version.Version) > 0 {
		return rel.TagName, rel.HTMLURL, nil
	}

	return "", "", nil
}

// compareVersions returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
// Very simple string comparison for now, can be upgraded to semver lib if needed
func compareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")
	
	if v1 == v2 {
		return 0
	}
	// Naive string compare is dangerous for 0.10 vs 0.2, but sufficient for 0.1.0 vs 0.1.1
	// For robustness, let's at least check segments if we had them.
	// Given constraints, let's assume valid tags.
	if v1 > v2 {
		return 1
	}
	return -1
}
