package setup

import "errors"

// InstallResult contains the outcome of installing an SDK package.
type InstallResult struct {
	SDKID   string `json:"sdk_id"`
	Package string `json:"package"`
	Version string `json:"version"`
	Command string `json:"command"`
	Success bool   `json:"success"`
}

// Installer runs the appropriate package manager command to add an SDK dependency.
type Installer interface {
	Install(dir string, detection *DetectResult) (*InstallResult, error)
}

// StubInstaller is a placeholder implementation. Replace with real install logic.
type StubInstaller struct{}

var _ Installer = StubInstaller{}

func (StubInstaller) Install(_ string, _ *DetectResult) (*InstallResult, error) {
	return nil, errors.New("install is not yet implemented: a real Installer must be provided")
}
