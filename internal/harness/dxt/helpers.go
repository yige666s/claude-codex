package dxt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Author struct {
	Name string `json:"name"`
}

type Manifest struct {
	Name   string `json:"name"`
	Author Author `json:"author"`
}

func ValidateManifest(manifest any) (Manifest, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, err
	}
	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return Manifest{}, fmt.Errorf("invalid manifest: name is required")
	}
	if strings.TrimSpace(parsed.Author.Name) == "" {
		return Manifest{}, fmt.Errorf("invalid manifest: author.name is required")
	}
	return parsed, nil
}

func ParseAndValidateManifestFromText(manifestText string) (Manifest, error) {
	var payload any
	if err := json.Unmarshal([]byte(manifestText), &payload); err != nil {
		return Manifest{}, fmt.Errorf("invalid JSON in manifest.json: %w", err)
	}
	return ValidateManifest(payload)
}

func ParseAndValidateManifestFromBytes(manifestData []byte) (Manifest, error) {
	return ParseAndValidateManifestFromText(string(manifestData))
}

func GenerateExtensionID(manifest Manifest, prefix string) string {
	sanitize := func(value string) string {
		value = strings.ToLower(value)
		value = regexp.MustCompile(`\s+`).ReplaceAllString(value, "-")
		value = regexp.MustCompile(`[^a-z0-9\-_.]`).ReplaceAllString(value, "")
		value = regexp.MustCompile(`-+`).ReplaceAllString(value, "-")
		return strings.Trim(value, "-")
	}

	author := sanitize(manifest.Author.Name)
	name := sanitize(manifest.Name)
	if prefix == "" {
		return author + "." + name
	}
	return prefix + "." + author + "." + name
}
