package agentruntime

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

const DefaultMaxAssetBytes int64 = 64 << 20

type AssetPolicy struct {
	MaxBytes          int64
	AllowedExtensions map[string]bool
	AllowedMIMETypes  map[string]bool
}

func DefaultAssetPolicy() AssetPolicy {
	extensions := []string{
		".png", ".jpg", ".jpeg", ".jfif", ".webp", ".gif", ".avif", ".bmp", ".tif", ".tiff", ".heic", ".heif", ".svg",
		".pdf",
		".txt", ".md", ".csv", ".json",
		".docx", ".xlsx", ".pptx",
	}
	mimeTypes := []string{
		"image/png", "image/jpeg", "image/pjpeg", "image/webp", "image/gif", "image/avif", "image/bmp", "image/tiff", "image/heic", "image/heif", "image/svg+xml",
		"application/pdf",
		"text/plain", "text/markdown", "text/csv", "application/json",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	return AssetPolicy{
		MaxBytes:          DefaultMaxAssetBytes,
		AllowedExtensions: mapSet(extensions),
		AllowedMIMETypes:  mapSet(mimeTypes),
	}
}

func (p AssetPolicy) withDefaults() AssetPolicy {
	defaults := DefaultAssetPolicy()
	if p.MaxBytes <= 0 {
		p.MaxBytes = defaults.MaxBytes
	}
	if len(p.AllowedExtensions) == 0 {
		p.AllowedExtensions = defaults.AllowedExtensions
	}
	if len(p.AllowedMIMETypes) == 0 {
		p.AllowedMIMETypes = defaults.AllowedMIMETypes
	}
	return p
}

func (p AssetPolicy) Validate(filename, contentType string, data []byte) (string, string, error) {
	return p.validate(filename, contentType, int64(len(data)), len(data) > 0, data)
}

func (p AssetPolicy) ValidateUpload(filename, contentType string, sizeBytes int64) (string, string, error) {
	return p.validate(filename, contentType, sizeBytes, false, nil)
}

func (p AssetPolicy) validate(filename, contentType string, sizeBytes int64, sniff bool, data []byte) (string, string, error) {
	p = p.withDefaults()
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return "", "", fmt.Errorf("filename is required")
	}
	if sizeBytes < 0 {
		return "", "", fmt.Errorf("file size is required")
	}
	if p.MaxBytes > 0 && sizeBytes > p.MaxBytes {
		return "", "", fmt.Errorf("file exceeds max size of %d bytes", p.MaxBytes)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || !p.AllowedExtensions[ext] {
		return "", "", fmt.Errorf("file extension %q is not allowed", ext)
	}
	contentType = normalizedContentType(contentType)
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = normalizedContentType(mime.TypeByExtension(ext))
	}
	if contentType == "" && sniff && len(data) > 0 {
		contentType = normalizedContentType(http.DetectContentType(data))
	}
	if contentType == "application/octet-stream" {
		contentType = normalizedContentType(mime.TypeByExtension(ext))
	}
	if contentType == "" {
		return "", "", fmt.Errorf("content type is required")
	}
	if !p.AllowedMIMETypes[contentType] {
		return "", "", fmt.Errorf("content type %q is not allowed", contentType)
	}
	return filename, contentType, nil
}

func mapSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func normalizedContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if idx := strings.Index(value, ";"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}
