package version

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version holds the current semantic version (major.minor.build).
var (
	Major = 0
	Minor = 0
	Build = 1
)

// String returns the version as "major.minor.build".
func String() string {
	return fmt.Sprintf("%d.%d.%d", Major, Minor, Build)
}

// versionFile returns the path to the version file next to the go.mod.
func versionFile() string {
	// Walk up from the executable or use known path
	candidates := []string{
		filepath.Join(projectRoot(), "VERSION"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Dir(c)); err == nil {
			return c
		}
	}
	return "VERSION"
}

func projectRoot() string {
	// Try to find the project root by looking for go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: relative to executable
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return filepath.Dir(resolved)
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// Load reads the version from the VERSION file and updates the package vars.
func Load() {
	data, err := os.ReadFile(versionFile())
	if err != nil {
		return
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ".")
	if len(parts) != 3 {
		return
	}
	if v, err := strconv.Atoi(parts[0]); err == nil {
		Major = v
	}
	if v, err := strconv.Atoi(parts[1]); err == nil {
		Minor = v
	}
	if v, err := strconv.Atoi(parts[2]); err == nil {
		Build = v
	}
}

// BumpBuild increments the build number and saves to VERSION file.
func BumpBuild() {
	Load()
	Build++
	save()
}

// BumpMinor increments minor, resets build.
func BumpMinor() {
	Load()
	Minor++
	Build = 0
	save()
}

// BumpMajor increments major, resets minor and build.
func BumpMajor() {
	Load()
	Major++
	Minor = 0
	Build = 0
	save()
}

func save() {
	os.WriteFile(versionFile(), []byte(String()), 0644)
}
