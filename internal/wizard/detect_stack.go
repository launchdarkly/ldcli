package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DetectStack scans dir for project indicator files and returns a ranked list
// of SDK IDs with the most likely match first. Returns nil if nothing is detected.
func DetectStack(dir string) []string {
	var detected []string

	// Check for package.json (Node.js / React / Next.js)
	if pkgBytes, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(pkgBytes, &pkg) == nil {
			allDeps := make(map[string]string)
			for k, v := range pkg.Dependencies {
				allDeps[k] = v
			}
			for k, v := range pkg.DevDependencies {
				allDeps[k] = v
			}
			if _, ok := allDeps["next"]; ok {
				// Next.js projects use the server-side SDK
				detected = append(detected, "node-server")
			} else if _, ok := allDeps["react"]; ok {
				detected = append(detected, "react-client-sdk")
			} else {
				detected = append(detected, "node-server")
			}
		}
	}

	// Check for go.mod (Go)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		detected = append(detected, "go-server-sdk")
	}

	// Check for Python project files
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		detected = append(detected, "python-server-sdk")
	} else if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		detected = append(detected, "python-server-sdk")
	}

	// Check for Java project files
	if _, err := os.Stat(filepath.Join(dir, "pom.xml")); err == nil {
		detected = append(detected, "java-server-sdk")
	} else if _, err := os.Stat(filepath.Join(dir, "build.gradle")); err == nil {
		detected = append(detected, "java-server-sdk")
	}

	return detected
}

// DetectPackageManager returns the most likely Node.js package manager for dir.
// Falls back to "npm" if nothing specific is found.
func DetectPackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	return "npm"
}
