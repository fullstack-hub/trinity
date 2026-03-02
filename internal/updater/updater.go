package updater

import (
	"fmt"
	"os/exec"
	"strings"
)

type UpdateResult struct {
	Name    string
	Updated bool
	Message string
}

func UpdateGemini(repoPath string) UpdateResult {
	cmd := exec.Command("git", "-C", repoPath, "fetch", "upstream")
	if err := cmd.Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("fetch failed: %v", err)}
	}

	out, _ := exec.Command("git", "-C", repoPath, "rev-list", "--count", "HEAD..upstream/main").Output()
	count := strings.TrimSpace(string(out))
	if count == "0" {
		return UpdateResult{Name: "gemini", Updated: false, Message: "already up to date"}
	}

	if err := exec.Command("git", "-C", repoPath, "merge", "upstream/main").Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("merge failed: %v", err)}
	}

	if err := exec.Command("npm", "run", "build", "--prefix", repoPath+"/packages/cli").Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("build failed: %v", err)}
	}

	return UpdateResult{Name: "gemini", Updated: true, Message: fmt.Sprintf("%s commits merged and rebuilt", count)}
}

func UpdateSDKServer(serverPath string, name string) UpdateResult {
	out, _ := exec.Command("npm", "outdated", "--json", "--prefix", serverPath).Output()
	if len(out) == 0 || string(out) == "{}\n" {
		return UpdateResult{Name: name, Updated: false, Message: "already up to date"}
	}

	if err := exec.Command("npm", "update", "--prefix", serverPath).Run(); err != nil {
		return UpdateResult{Name: name, Updated: false, Message: fmt.Sprintf("update failed: %v", err)}
	}

	return UpdateResult{Name: name, Updated: true, Message: "SDK updated"}
}
