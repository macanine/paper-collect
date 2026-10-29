package office

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
)

func findSOffice() (string, error) {
	return exec.LookPath("soffice")
}

func (c *Converter) convert(ctx context.Context, work, source string) (string, error) {
	profile := filepath.Join(work, "profile")
	if err := os.MkdirAll(filepath.Join(profile, "user"), 0o700); err != nil {
		return "", apperr.Internal(err)
	}
	// Use a new profile per conversion and the highest macro security level.
	config := `<?xml version="1.0" encoding="UTF-8"?><oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item></oor:items>`
	if err := os.WriteFile(filepath.Join(profile, "user", "registrymodifications.xcu"), []byte(config), 0o600); err != nil {
		return "", apperr.Internal(err)
	}
	absProfile, err := filepath.Abs(profile)
	if err != nil {
		return "", apperr.Internal(err)
	}
	profilePath := filepath.ToSlash(absProfile)
	if runtime.GOOS == "windows" && !strings.HasPrefix(profilePath, "/") {
		profilePath = "/" + profilePath
	}
	profileURL := (&url.URL{Scheme: "file", Path: profilePath}).String()

	convertCtx, cancel := context.WithTimeout(ctx, conversionLimit)
	defer cancel()
	cmd := exec.CommandContext(convertCtx, "soffice", "-env:UserInstallation="+profileURL, "--headless", "--nologo", "--nodefault", "--nofirststartwizard", "--nolockcheck", "--convert-to", "pdf:writer_pdf_Export", "--outdir", work, source)
	cmd.Env = rendererEnvironment()
	fonts, err := fontConfig(work, c.dataDir)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if fonts != "" {
		cmd.Env = append(cmd.Env, "FONTCONFIG_FILE="+fonts)
	}
	// LibreOffice output can contain document names or contents; do not log it.
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(convertCtx.Err(), context.DeadlineExceeded) {
			return "", apperr.Unavailable("DOCX 转换超时，请检查文档大小和复杂度")
		}
		return "", apperr.Unprocessable("DOCX 无法转换为预览，请检查文档是否损坏或加密")
	}
	return filepath.Join(work, "document.pdf"), nil
}

// Do not pass application credentials or database connection strings to the
// renderer.
func rendererEnvironment() []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
		"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TMPDIR": true,
		"FONTCONFIG_FILE": true, "FONTCONFIG_PATH": true,
		"XDG_CACHE_HOME": true, "XDG_RUNTIME_DIR": true,
		"DYLD_LIBRARY_PATH": true, "LD_LIBRARY_PATH": true,
		"Path": true, "SystemRoot": true, "WINDIR": true, "TEMP": true, "TMP": true,
		"USERPROFILE": true, "PATHEXT": true, "COMSPEC": true,
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[key] {
			env = append(env, entry)
		}
	}
	return env
}
