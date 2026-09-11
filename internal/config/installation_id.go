package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

const installationIDKey = "installation-id"

func EnsureInstallationID(filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("config filename is required")
	}

	values, mode, err := readConfigValues(filename)
	if err != nil {
		return "", err
	}

	if value, ok := values[installationIDKey]; ok {
		installationID, ok := value.(string)
		if !ok || uuid.Validate(installationID) != nil {
			return "", errors.New("ldcli installation ID in config is invalid")
		}

		return installationID, nil
	}

	installationID := uuid.NewString()
	values[installationIDKey] = installationID

	if err := writeConfigValues(filename, values, mode); err != nil {
		return "", err
	}

	return installationID, nil
}

func readConfigValues(filename string) (map[string]any, os.FileMode, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read ldcli config: %w", err)
	}

	values := map[string]any{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, 0, fmt.Errorf("parse ldcli config: %w", err)
	}

	info, err := os.Stat(filename)
	if err != nil {
		return nil, 0, fmt.Errorf("stat ldcli config: %w", err)
	}

	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o600
	}

	return values, mode, nil
}

func writeConfigValues(
	filename string,
	values map[string]any,
	mode os.FileMode,
) error {
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("marshal ldcli config: %w", err)
	}

	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ldcli config directory: %w", err)
	}

	file, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary ldcli config: %w", err)
	}

	tempName := file.Name()
	defer func() {
		_ = os.Remove(tempName)
	}()

	if err := file.Chmod(mode); err != nil {
		_ = file.Close()

		return fmt.Errorf("set ldcli config permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return fmt.Errorf("write ldcli config: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()

		return fmt.Errorf("sync ldcli config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close ldcli config: %w", err)
	}
	if err := os.Rename(tempName, filename); err != nil {
		return fmt.Errorf("replace ldcli config: %w", err)
	}

	return nil
}
