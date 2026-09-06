package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Manifest struct {
	Name string `json:"name"`
	Run  string `json:"run"`
}

func ReadManifest(dir string) (Manifest, error) {
	path := filepath.Join(dir, "aether.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("host: read %s: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("host: parse %s: %w", path, err)
	}
	if err := validateManifest(m); err != nil {
		return Manifest{}, fmt.Errorf("host: %s: %w", path, err)
	}
	return m, nil
}

func validateManifest(m Manifest) error {
	if m.Name == "" {
		return errors.New("missing \"name\"")
	}
	if m.Run == "" {
		return errors.New("missing \"run\"")
	}
	return nil
}
