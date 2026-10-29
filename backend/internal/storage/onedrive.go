package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const oneDriveChunkSize = 10 << 20 // 32 × 320 KiB, below Graph's 60 MiB limit.
const oneDriveOpenListRenewURL = "https://api.oplist.org/onedrive/renewapi"

type oneDriveAdapter struct {
	client                                                                  *http.Client
	clientID, clientSecret, tenant, refreshToken, shareURL, scope, authMode string
	prefix                                                                  []string
	saveRefresh                                                             func(context.Context, string, string) error
	mu                                                                      sync.Mutex
	accessToken                                                             string
	expires                                                                 time.Time
}

type graphItem struct {
	ID     string          `json:"id"`
	Size   *int64          `json:"size"`
	Folder json.RawMessage `json:"folder"`
	Parent struct {
		DriveID string `json:"driveId"`
	} `json:"parentReference"`
}

// NewOneDrive uses delegated Graph permissions; an optional shared folder link
// is resolved by Graph and must grant the signed-in account write access.
func NewOneDrive(cfg Config, client *http.Client) (Adapter, error) {
	id := strings.TrimSpace(cfg.Settings["client_id"])
	refresh := cfg.Secrets["refresh_token"]
	authMode := strings.TrimSpace(cfg.Settings["auth_mode"])
	if authMode == "" {
		authMode = "direct" // Existing providers use their own Microsoft app.
	}
	if authMode != "direct" && authMode != "openlist" {
		return nil, newCloudError(ClassConfig, "onedrive_auth_mode_invalid", false)
	}
	if refresh == "" || (authMode == "direct" && id == "") {
		return nil, newCloudError(ClassConfig, "onedrive_credentials_required", false)
	}
	tenant := strings.TrimSpace(cfg.Settings["tenant"])
	if tenant == "" {
		tenant = "common"
	}
	if len(tenant) > 128 || strings.Trim(tenant, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-.") != "" {
		return nil, newCloudError(ClassConfig, "onedrive_tenant_invalid", false)
	}
	prefix, cerr := validatePathPrefix(cfg.Settings["path_prefix"])
	if cerr != nil {
		return nil, cerr
	}
	scope := cfg.Settings["share_scope"]
	if scope == "" {
		scope = "anonymous"
	}
	if scope != "anonymous" && scope != "organization" && scope != "disabled" {
		return nil, newCloudError(ClassConfig, "onedrive_share_scope_invalid", false)
	}
	shareURL := strings.TrimSpace(cfg.Secrets["share_url"])
	if shareURL != "" {
		u, err := url.Parse(shareURL)
		if err != nil || u.Scheme != "https" || u.User != nil || !microsoftShareHost(u.Hostname()) {
			return nil, newCloudError(ClassConfig, "onedrive_share_url_invalid", false)
		}
	}
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	// Pre-authenticated download redirects must not receive the Graph bearer token.
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[len(via)-1].URL.Host) {
			req.Header.Del("Authorization")
		}
		if req.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &oneDriveAdapter{client: &copyClient, clientID: id, clientSecret: cfg.Secrets["client_secret"], tenant: tenant, refreshToken: refresh, shareURL: shareURL, scope: scope, authMode: authMode, prefix: prefix, saveRefresh: cfg.SaveRefreshToken}, nil
}

func microsoftShareHost(host string) bool {
	host = strings.ToLower(host)
	return host == "1drv.ms" || host == "onedrive.live.com" || host == "onedrive.com" || strings.HasSuffix(host, ".onedrive.com") || strings.HasSuffix(host, ".sharepoint.com")
}
func microsoftUploadHost(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, ".1drv.com") || strings.HasSuffix(host, ".onedrive.com") || strings.HasSuffix(host, ".sharepoint.com")
}

func (a *oneDriveAdapter) token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessToken != "" && time.Now().Before(a.expires.Add(-time.Minute)) {
		return a.accessToken, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var req *http.Request
	if a.authMode == "openlist" {
		// OpenList's documented renewal API requires the refresh token as a
		// query parameter. Never log the URL or forward it across redirects.
		params := url.Values{"refresh_ui": {a.refreshToken}, "server_use": {"true"}, "driver_txt": {"onedrive_pr"}}
		req, _ = http.NewRequestWithContext(requestCtx, http.MethodGet, oneDriveOpenListRenewURL+"?"+params.Encode(), nil)
	} else {
		form := url.Values{"client_id": {a.clientID}, "refresh_token": {a.refreshToken}, "grant_type": {"refresh_token"}, "scope": {"offline_access Files.ReadWrite"}}
		if a.clientSecret != "" {
			form.Set("client_secret", a.clientSecret)
		}
		req, _ = http.NewRequestWithContext(requestCtx, http.MethodPost, "https://login.microsoftonline.com/"+url.PathEscape(a.tenant)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", transportError(ctx, err, "onedrive_token")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if a.authMode == "openlist" {
			var upstream struct {
				Text string `json:"text"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&upstream) == nil {
				text := strings.ToLower(upstream.Text)
				if strings.Contains(text, "invalid_grant") || strings.Contains(text, "aadsts700082") || strings.Contains(text, "aadsts700084") || strings.Contains(text, "aadsts50173") {
					return "", newCloudError(ClassAuth, "onedrive_token_rejected", false)
				}
			}
		}
		if resp.StatusCode == http.StatusBadRequest {
			return "", newCloudError(ClassAuth, "onedrive_token_rejected", false)
		}
		return "", statusError("onedrive_token", resp.StatusCode)
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.AccessToken == "" {
		return "", newCloudError(ClassUnavailable, "onedrive_token_response_invalid", true)
	}
	if result.RefreshToken != "" && result.RefreshToken != a.refreshToken {
		if a.saveRefresh != nil {
			if err := a.saveRefresh(ctx, a.refreshToken, result.RefreshToken); err != nil {
				return "", newCloudError(ClassUnavailable, "onedrive_token_save_failed", true)
			}
		}
		a.refreshToken = result.RefreshToken
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 3600
	}
	a.accessToken, a.expires = result.AccessToken, time.Now().Add(time.Duration(result.ExpiresIn)*time.Second)
	return a.accessToken, nil
}

func (a *oneDriveAdapter) graph(ctx context.Context, method, target string, body io.Reader, op string) (*http.Response, error) {
	token, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if op == "onedrive_open" {
		timeout = 5 * time.Minute
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(requestCtx, method, "https://graph.microsoft.com/v1.0"+target, body)
	if err != nil {
		cancel()
		return nil, newCloudError(ClassConfig, op+"_request_invalid", false)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.HasPrefix(target, "/shares/") {
		req.Header.Set("Prefer", "redeemSharingLinkIfNecessary")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		cancel()
		return nil, transportError(ctx, err, op)
	}
	resp.Body = cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func graphReadItem(resp *http.Response, op string) (graphItem, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return graphItem{}, statusError(op, resp.StatusCode)
	}
	var item graphItem
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&item) != nil || item.ID == "" {
		return graphItem{}, newCloudError(ClassUnavailable, op+"_response_invalid", true)
	}
	return item, nil
}

func (a *oneDriveAdapter) root(ctx context.Context) (string, string, error) {
	target := "/me/drive/root"
	if a.shareURL != "" {
		target = "/shares/u!" + base64.RawURLEncoding.EncodeToString([]byte(a.shareURL)) + "/driveItem"
	}
	resp, err := a.graph(ctx, http.MethodGet, target, nil, "onedrive_root")
	if err != nil {
		return "", "", err
	}
	item, err := graphReadItem(resp, "onedrive_root")
	if err != nil {
		return "", "", err
	}
	if item.Parent.DriveID == "" || len(item.Folder) == 0 || string(item.Folder) == "null" {
		return "", "", newCloudError(ClassConfig, "onedrive_folder_required", false)
	}
	return item.Parent.DriveID, item.ID, nil
}
func graphItemPath(drive, item string) string {
	return "/drives/" + url.PathEscape(drive) + "/items/" + url.PathEscape(item)
}
func oneDriveName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:*?"<>|#%`, r) {
			return '_'
		}
		return r
	}, name)
}

func (a *oneDriveAdapter) folder(ctx context.Context, drive, parent string, parts []string) (string, error) {
	for _, raw := range parts {
		name := oneDriveName(raw)
		target := graphItemPath(drive, parent) + ":/" + url.PathEscape(name)
		resp, err := a.graph(ctx, http.MethodGet, target, nil, "onedrive_folder")
		if err != nil {
			return "", err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			payload, _ := json.Marshal(map[string]any{"name": name, "folder": map[string]any{}, "@microsoft.graph.conflictBehavior": "fail"})
			resp, err = a.graph(ctx, http.MethodPost, graphItemPath(drive, parent)+"/children", bytes.NewReader(payload), "onedrive_mkdir")
			if err != nil {
				return "", err
			}
			if resp.StatusCode == http.StatusConflict {
				resp.Body.Close()
				resp, err = a.graph(ctx, http.MethodGet, target, nil, "onedrive_folder")
				if err != nil {
					return "", err
				}
			}
		}
		item, err := graphReadItem(resp, "onedrive_folder")
		if err != nil {
			return "", err
		}
		if len(item.Folder) == 0 || string(item.Folder) == "null" {
			return "", newCloudError(ClassConfig, "onedrive_path_not_folder", false)
		}
		parent = item.ID
	}
	return parent, nil
}

func (a *oneDriveAdapter) Put(ctx context.Context, obj Object) (Stored, error) {
	segments, cerr := validateObjectKey(obj.Key)
	if cerr != nil {
		return Stored{}, cerr
	}
	if obj.Size <= 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "onedrive_size_invalid", false)
	}
	if obj.SHA256 != "" {
		if _, ok := normalizeSHA256(obj.SHA256); !ok {
			return Stored{}, newCloudError(ClassInvalidInput, "sha256_invalid", false)
		}
	}
	f, err := os.Open(obj.Path)
	if err != nil {
		return Stored{}, newCloudError(ClassFileMissing, "onedrive_source_missing", false)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Stored{}, newCloudError(ClassInvalidInput, "onedrive_source_invalid", false)
	}
	if info.Size() != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "onedrive_source_size_mismatch", false)
	}
	drive, root, err := a.root(ctx)
	if err != nil {
		return Stored{}, err
	}
	parts := append(append([]string{}, a.prefix...), segments[:len(segments)-1]...)
	parent, err := a.folder(ctx, drive, root, parts)
	if err != nil {
		return Stored{}, err
	}
	name := oneDriveName(segments[len(segments)-1])
	payloadJSON, _ := json.Marshal(map[string]any{"item": map[string]string{"@microsoft.graph.conflictBehavior": "replace", "name": name}})
	payload := bytes.NewReader(payloadJSON)
	resp, err := a.graph(ctx, http.MethodPost, graphItemPath(drive, parent)+":/"+url.PathEscape(name)+":/createUploadSession", payload, "onedrive_upload_session")
	if err != nil {
		return Stored{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := resp.StatusCode
		resp.Body.Close()
		return Stored{}, statusError("onedrive_upload_session", status)
	}
	var session struct {
		UploadURL string `json:"uploadUrl"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&session)
	resp.Body.Close()
	u, parseErr := url.Parse(session.UploadURL)
	if err != nil || parseErr != nil || u.Scheme != "https" || u.User != nil || !microsoftUploadHost(u.Hostname()) {
		return Stored{}, newCloudError(ClassUnavailable, "onedrive_upload_session_invalid", false)
	}
	var item graphItem
	for start := int64(0); start < obj.Size; {
		n := int64(oneDriveChunkSize)
		if obj.Size-start < n {
			n = obj.Size - start
		}
		partCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		req, _ := http.NewRequestWithContext(partCtx, http.MethodPut, u.String(), io.NewSectionReader(f, start, n))
		req.ContentLength = n
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+n-1, obj.Size))
		req.Header.Set("Content-Type", "application/octet-stream")
		part, err := a.client.Do(req)
		if err != nil {
			cancel()
			return Stored{}, transportError(ctx, err, "onedrive_upload")
		}
		part.Body = cancelBody{ReadCloser: part.Body, cancel: cancel}
		if part.StatusCode == http.StatusAccepted {
			part.Body.Close()
			start += n
			if obj.Progress != nil {
				obj.Progress(start, obj.Size)
			}
			continue
		}
		if part.StatusCode < 200 || part.StatusCode >= 300 {
			status := part.StatusCode
			part.Body.Close()
			return Stored{}, statusError("onedrive_upload", status)
		}
		item, err = graphReadItem(part, "onedrive_upload")
		if err != nil {
			return Stored{}, err
		}
		start += n
		if obj.Progress != nil {
			obj.Progress(start, obj.Size)
		}
		if start != obj.Size {
			return Stored{}, newCloudError(ClassUnavailable, "onedrive_upload_early_completion", false)
		}
	}
	if item.ID == "" || item.Size == nil || *item.Size != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "onedrive_upload_size_mismatch", false)
	}
	return Stored{RemoteID: base64.RawURLEncoding.EncodeToString([]byte(drive)) + "." + base64.RawURLEncoding.EncodeToString([]byte(item.ID)), Size: obj.Size, Verification: "size"}, nil
}

func decodeOneDriveID(remote string) (string, string, error) {
	parts := strings.Split(remote, ".")
	if len(parts) != 2 {
		return "", "", newCloudError(ClassInvalidInput, "onedrive_remote_id_invalid", false)
	}
	drive, e1 := base64.RawURLEncoding.DecodeString(parts[0])
	item, e2 := base64.RawURLEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(drive) == 0 || len(item) == 0 {
		return "", "", newCloudError(ClassInvalidInput, "onedrive_remote_id_invalid", false)
	}
	return string(drive), string(item), nil
}
func (a *oneDriveAdapter) Open(ctx context.Context, remote string) (io.ReadCloser, error) {
	drive, item, err := decodeOneDriveID(remote)
	if err != nil {
		return nil, err
	}
	resp, err := a.graph(ctx, http.MethodGet, graphItemPath(drive, item)+"/content", nil, "onedrive_open")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := resp.StatusCode
		resp.Body.Close()
		return nil, statusError("onedrive_open", status)
	}
	return resp.Body, nil
}
func (a *oneDriveAdapter) Share(ctx context.Context, remote string) (Share, error) {
	if a.scope == "disabled" {
		return Share{Status: "unsupported"}, nil
	}
	drive, item, err := decodeOneDriveID(remote)
	if err != nil {
		return Share{}, err
	}
	payload, _ := json.Marshal(map[string]string{"type": "view", "scope": a.scope})
	resp, err := a.graph(ctx, http.MethodPost, graphItemPath(drive, item)+"/createLink", bytes.NewReader(payload), "onedrive_share")
	if err != nil {
		return Share{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Share{}, statusError("onedrive_share", resp.StatusCode)
	}
	var result struct {
		Link struct {
			WebURL string `json:"webUrl"`
		} `json:"link"`
		Expiration *time.Time `json:"expirationDateTime"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.Link.WebURL == "" {
		return Share{}, newCloudError(ClassUnavailable, "onedrive_share_response_invalid", false)
	}
	return Share{URL: result.Link.WebURL, ExpiresAt: result.Expiration, Status: "ready"}, nil
}
func (a *oneDriveAdapter) Health(ctx context.Context) error { _, _, err := a.root(ctx); return err }
func (a *oneDriveAdapter) Capabilities() Capabilities {
	return Capabilities{Upload: true, Download: true, Share: a.scope != "disabled"}
}
