//go:build darwin

package office

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
)

// Some headless LibreOffice distributions on macOS ship a fontconfig that does
// not discover installed CJK fonts. Supply explicit system font paths unless
// the operator already selected a fontconfig configuration.
func fontConfig(work, dataDir string) (string, error) {
	if os.Getenv("FONTCONFIG_FILE") != "" {
		return "", nil
	}
	dirs := []string{"/System/Library/Fonts", "/Library/Fonts"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
	}
	cache := filepath.Join(dataDir, "tmp", "fontconfig-cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return "", err
	}
	var config bytes.Buffer
	config.WriteString(`<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig>`)
	for _, dir := range dirs {
		config.WriteString("<dir>")
		_ = xml.EscapeText(&config, []byte(dir))
		config.WriteString("</dir>")
	}
	config.WriteString("<cachedir>")
	_ = xml.EscapeText(&config, []byte(cache))
	config.WriteString("</cachedir></fontconfig>")
	name := filepath.Join(work, "fonts.conf")
	if err := os.WriteFile(name, config.Bytes(), 0o600); err != nil {
		return "", err
	}
	return name, nil
}
