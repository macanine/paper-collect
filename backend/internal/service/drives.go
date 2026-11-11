package service

import (
	"context"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/jobexec"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"github.com/nic/paper-collect/backend/internal/storage"
	"gorm.io/gorm"
)

const (
	maxDriveNameRunes  = 128
	driveHealthTimeout = 30 * time.Second
)

// DriveInput is the validated body of a drive create request.
type DriveInput struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Enabled      bool              `json:"enabled"`
	RootPath     string            `json:"root_path"`
	MaxFileBytes int64             `json:"max_file_bytes"`
	AllowedExts  []string          `json:"allowed_exts"`
	ReadOrder    int               `json:"read_order"`
	OnPathChange string            `json:"on_path_change"`
	RouteMode    string            `json:"route_mode"`
	Routes       []DriveRouteInput `json:"routes"`
	Config       map[string]string `json:"config"`
	Secret       map[string]string `json:"secret"`
}

type DrivePatch struct {
	Name         *string            `json:"name"`
	Enabled      *bool              `json:"enabled"`
	RootPath     *string            `json:"root_path"`
	MaxFileBytes *int64             `json:"max_file_bytes"`
	AllowedExts  *[]string          `json:"allowed_exts"`
	ReadOrder    *int               `json:"read_order"`
	OnPathChange *string            `json:"on_path_change"`
	RouteMode    *string            `json:"route_mode"`
	Routes       *[]DriveRouteInput `json:"routes"`
	Config       *map[string]string `json:"config"`
	Secret       *map[string]string `json:"secret"`
}

// DriveRouteInput is one node assignment submitted with a drive.
type DriveRouteInput struct {
	NodeID   string
	Priority int
	Enabled  bool
}

// DriveRouteView is the safe, display-oriented form returned to the UI.
type DriveRouteView struct {
	NodeID   string
	NodeName string
	IsMaster bool
	Priority int
	Enabled  bool
	Status   string
}

// DriveTest is the result of an explicit adapter health check.
type DriveTest struct {
	Healthy      bool
	Message      string
	Capabilities storage.Capabilities
}

var driveConfigKeys = map[string]map[string]bool{
	model.KindLocal:    {},
	model.KindWebDAV:   {"base_url": true, "public_base_url": true},
	model.KindLanzou:   {"base_url": true, "verify_remote_hash": true},
	model.KindPan123:   {"base_url": true, "parent_file_id": true},
	model.KindS3:       {"endpoint": true, "region": true, "bucket": true, "path_style": true, "share_expires_seconds": true},
	model.KindOneDrive: {"auth_mode": true, "client_id": true, "tenant": true, "share_scope": true},
}

var driveSecretKeys = map[string]map[string]bool{
	model.KindLocal:    {},
	model.KindWebDAV:   {"username": true, "password": true},
	model.KindLanzou:   {"cookie": true},
	model.KindPan123:   {"client_id": true, "client_secret": true},
	model.KindS3:       {"access_key_id": true, "secret_access_key": true, "session_token": true},
	model.KindOneDrive: {"client_secret": true, "refresh_token": true, "share_url": true},
}

// ListDrives returns every endpoint, including disabled ones, by read priority.
func (s *Service) ListDrives(ctx context.Context) ([]model.Drive, error) {
	items := []model.Drive{}
	if err := s.db.WithContext(ctx).Order("read_order ASC").Order("created_at ASC").Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	return items, nil
}

// ListDriveRoutes returns the ordered upload nodes for one drive. Node tokens
// and other credentials are never part of this view.
func (s *Service) ListDriveRoutes(ctx context.Context, driveID string) ([]DriveRouteView, error) {
	var routes []model.DriveNodeRoute
	if err := s.db.WithContext(ctx).Where("drive_id = ?", driveID).
		Order("priority ASC").Order("node_id ASC").Find(&routes).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	if len(routes) == 0 {
		return []DriveRouteView{}, nil
	}
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.NodeID)
	}
	var nodes []model.Node
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&nodes).Error; err != nil {
		return nil, apperr.Internal(err)
	}
	byID := make(map[string]model.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	out := make([]DriveRouteView, 0, len(routes))
	for _, route := range routes {
		node, ok := byID[route.NodeID]
		if !ok {
			continue
		}
		out = append(out, DriveRouteView{
			NodeID: route.NodeID, NodeName: node.Name, IsMaster: node.IsMaster,
			Priority: route.Priority, Enabled: route.Enabled, Status: s.nodeStatus(node),
		})
	}
	return out, nil
}

// CreateDrive stores a new endpoint and verifies its adapter configuration.
func (s *Service) CreateDrive(ctx context.Context, p auth.Principal, in DriveInput) (model.Drive, error) {
	if err := validateDriveInput(&in); err != nil {
		return model.Drive{}, err
	}
	if err := validateDriveMaps(in.Kind, in.Config, in.Secret); err != nil {
		return model.Drive{}, err
	}
	routeMode, routes, err := s.validateDriveRoutes(ctx, in.Kind, in.RouteMode, in.Routes)
	if err != nil {
		return model.Drive{}, err
	}
	id := newID()
	if _, err := s.buildDriveAdapter(in.Kind, id, in.Config, in.Secret); err != nil {
		return model.Drive{}, apperr.Validation("网盘配置无效: " + jobexec.StorageErrorText(err))
	}
	cipher, err := s.sealSecret(id, in.Secret)
	if err != nil {
		return model.Drive{}, apperr.Internal(err)
	}
	now := s.now()
	drive := model.Drive{
		ID:       id,
		Name:     strings.TrimSpace(in.Name),
		Kind:     in.Kind,
		Enabled:  in.Enabled,
		RootPath: in.RootPath, MaxFileBytes: in.MaxFileBytes, AllowedExtsJSON: model.EncodeStrings(in.AllowedExts), ReadOrder: in.ReadOrder, OnPathChange: in.OnPathChange,
		ConfigJSON: model.EncodeMap(in.Config), RouteMode: routeMode,
		SecretCipher: cipher,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&drive).Error; err != nil {
			return err
		}
		if err := replaceDriveRoutes(tx, drive.ID, routes); err != nil {
			return err
		}
		return s.audit(tx, p, "storage.drive_create", drive.ID)
	})
	if err != nil {
		return model.Drive{}, apperr.Internal(err)
	}
	return drive, nil
}

// UpdateDrive replaces the supplied fields. Config and secret are whole-object
// replacements when present; an empty secret object clears it. Kind cannot change.
func (s *Service) UpdateDrive(ctx context.Context, p auth.Principal, id string, pt DrivePatch) (model.Drive, error) {
	db := s.db.WithContext(ctx)
	var drive model.Drive
	if err := db.Where("id = ?", id).First(&drive).Error; err != nil {
		return model.Drive{}, apperr.NotFound("网盘不存在")
	}

	in := DriveInput{Name: drive.Name, Kind: drive.Kind, Enabled: drive.Enabled, RootPath: drive.RootPath, MaxFileBytes: drive.MaxFileBytes, AllowedExts: model.DecodeStrings(drive.AllowedExtsJSON), ReadOrder: drive.ReadOrder, OnPathChange: drive.OnPathChange, RouteMode: drive.RouteMode}
	if pt.Name != nil {
		in.Name = *pt.Name
	}
	if pt.Enabled != nil {
		in.Enabled = *pt.Enabled
	}
	if pt.RootPath != nil {
		in.RootPath = *pt.RootPath
	}
	if pt.MaxFileBytes != nil {
		in.MaxFileBytes = *pt.MaxFileBytes
	}
	if pt.AllowedExts != nil {
		in.AllowedExts = *pt.AllowedExts
	}
	if pt.ReadOrder != nil {
		in.ReadOrder = *pt.ReadOrder
	}
	if pt.OnPathChange != nil {
		in.OnPathChange = *pt.OnPathChange
	}
	var existingRoutes []model.DriveNodeRoute
	if err := db.Where("drive_id = ?", drive.ID).Order("priority ASC").Find(&existingRoutes).Error; err != nil {
		return model.Drive{}, apperr.Internal(err)
	}
	in.Routes = make([]DriveRouteInput, 0, len(existingRoutes))
	for _, route := range existingRoutes {
		in.Routes = append(in.Routes, DriveRouteInput{NodeID: route.NodeID, Priority: route.Priority, Enabled: route.Enabled})
	}
	if pt.RouteMode != nil {
		in.RouteMode = *pt.RouteMode
	}
	if pt.Routes != nil {
		in.Routes = *pt.Routes
	}
	if err := validateDriveInput(&in); err != nil {
		return model.Drive{}, err
	}

	cfg := model.DecodeMap(drive.ConfigJSON)
	oldAuthMode := cfg["auth_mode"]
	if oldAuthMode == "" {
		oldAuthMode = "direct"
	}
	if pt.Config != nil {
		cfg = *pt.Config
	}
	newAuthMode := cfg["auth_mode"]
	if newAuthMode == "" {
		newAuthMode = "direct"
	}
	if drive.Kind == model.KindOneDrive && oldAuthMode != newAuthMode && pt.Secret == nil {
		return model.Drive{}, apperr.Validation("切换 OneDrive 授权方式时，请填写对应的刷新令牌")
	}
	secret := map[string]string{}
	if drive.SecretCipher != "" {
		raw, err := s.box.Open(drive.SecretCipher, []byte(drive.ID))
		if err != nil {
			return model.Drive{}, apperr.Internal(err)
		}
		secret = model.DecodeMap(string(raw))
	}
	if drive.Kind == model.KindOneDrive && oldAuthMode != newAuthMode && pt.Secret != nil && (*pt.Secret)["refresh_token"] == secret["refresh_token"] {
		return model.Drive{}, apperr.Validation("切换 OneDrive 授权方式时，请填写新应用对应的刷新令牌")
	}
	if pt.Secret != nil {
		secret = *pt.Secret
	}
	if err := validateDriveMaps(drive.Kind, cfg, secret); err != nil {
		return model.Drive{}, err
	}
	routeMode, routes, err := s.validateDriveRoutes(ctx, drive.Kind, in.RouteMode, in.Routes)
	if err != nil {
		return model.Drive{}, err
	}
	if _, err := s.buildDriveAdapter(drive.Kind, drive.ID, cfg, secret); err != nil {
		return model.Drive{}, apperr.Validation("网盘配置无效: " + jobexec.StorageErrorText(err))
	}

	updates := map[string]any{
		"name":      strings.TrimSpace(in.Name),
		"root_path": in.RootPath, "max_file_bytes": in.MaxFileBytes, "allowed_exts_json": model.EncodeStrings(in.AllowedExts), "on_path_change": in.OnPathChange,
		"enabled":     in.Enabled,
		"read_order":  in.ReadOrder,
		"config_json": model.EncodeMap(cfg), "route_mode": routeMode,
		"updated_at": s.now(),
	}
	if pt.Secret != nil {
		cipher, err := s.sealSecret(drive.ID, secret)
		if err != nil {
			return model.Drive{}, apperr.Internal(err)
		}
		updates["secret_cipher"] = cipher
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Drive{}).Where("id = ?", drive.ID).Updates(updates).Error; err != nil {
			return err
		}
		if pt.RouteMode != nil || pt.Routes != nil {
			if err := replaceDriveRoutes(tx, drive.ID, routes); err != nil {
				return err
			}
		}
		return s.audit(tx, p, "storage.drive_update", drive.ID)
	})
	if err != nil {
		return model.Drive{}, apperr.Internal(err)
	}
	if err := db.Where("id = ?", drive.ID).First(&drive).Error; err != nil {
		return model.Drive{}, apperr.Internal(err)
	}
	s.notify()
	return drive, nil
}

// TestDrive performs a real adapter health check. A failed connection is a
// successful API response with healthy=false, never a fabricated success.
func (s *Service) TestDrive(ctx context.Context, id string) (DriveTest, error) {
	var drive model.Drive
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&drive).Error; err != nil {
		return DriveTest{}, apperr.NotFound("网盘不存在")
	}
	adapter, err := s.adapterFor(drive)
	if err != nil {
		return DriveTest{Healthy: false, Message: jobexec.StorageErrorText(err), Capabilities: storage.Capabilities{}}, nil
	}
	caps := adapter.Capabilities()
	healthCtx, cancel := context.WithTimeout(ctx, driveHealthTimeout)
	defer cancel()
	if err := adapter.Health(healthCtx); err != nil {
		return DriveTest{Healthy: false, Message: jobexec.StorageErrorText(err), Capabilities: caps}, nil
	}
	return DriveTest{Healthy: true, Message: "连接成功", Capabilities: caps}, nil
}

func (s *Service) validateDriveRoutes(ctx context.Context, kind, rawMode string, routes []DriveRouteInput) (string, []DriveRouteInput, error) {
	mode := strings.TrimSpace(rawMode)
	if mode == "" {
		mode = model.DriveRoutePool
	}
	if !isOneOf(mode, model.DriveRouteModes...) {
		return "", nil, apperr.Validation("上传节点策略无效")
	}
	if kind == model.KindLocal {
		if mode != model.DriveRoutePool || len(routes) > 0 {
			return "", nil, apperr.Validation("local 网盘只能由主端执行，不能配置从端路由")
		}
		return model.DriveRoutePool, []DriveRouteInput{}, nil
	}
	if mode == model.DriveRoutePool {
		return mode, []DriveRouteInput{}, nil
	}
	if len(routes) == 0 || len(routes) > 32 {
		return "", nil, apperr.Validation("节点路由需要 1 到 32 个节点")
	}
	seenNode := map[string]bool{}
	seenPriority := map[int]bool{}
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		route.NodeID = strings.TrimSpace(route.NodeID)
		if route.NodeID == "" || seenNode[route.NodeID] {
			return "", nil, apperr.Validation("节点路由不能重复且必须指定节点")
		}
		if route.Priority < 1 || route.Priority > 1000 || seenPriority[route.Priority] {
			return "", nil, apperr.Validation("节点优先级必须为 1 到 1000 且不能重复")
		}
		seenNode[route.NodeID] = true
		seenPriority[route.Priority] = true
		ids = append(ids, route.NodeID)
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Node{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return "", nil, apperr.Internal(err)
	}
	if count != int64(len(ids)) {
		return "", nil, apperr.Validation("节点路由包含不存在的节点")
	}
	return mode, routes, nil
}

func replaceDriveRoutes(tx *gorm.DB, driveID string, routes []DriveRouteInput) error {
	if err := tx.Where("drive_id = ?", driveID).Delete(&model.DriveNodeRoute{}).Error; err != nil {
		return err
	}
	for _, route := range routes {
		if err := tx.Create(&model.DriveNodeRoute{
			ID: newID(), DriveID: driveID, NodeID: route.NodeID,
			Priority: route.Priority, Enabled: route.Enabled,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateDriveMaps(kind string, config, secret map[string]string) error {
	for key := range config {
		if !driveConfigKeys[kind][key] {
			return apperr.Validation("存储配置包含该类型不支持的字段: " + key)
		}
	}
	for key := range secret {
		if !driveSecretKeys[kind][key] {
			return apperr.Validation("存储密钥包含该类型不支持的字段: " + key)
		}
	}
	return nil
}

func (s *Service) sealSecret(driveID string, secret map[string]string) (string, error) {
	if len(secret) == 0 {
		return "", nil
	}
	return s.box.Seal([]byte(model.EncodeMap(secret)), []byte(driveID))
}

// buildDriveAdapter constructs an adapter for validation or execution. Local
// roots are fixed by the server and can never come from the API.
func (s *Service) buildDriveAdapter(kind, id string, config, secret map[string]string) (storage.Adapter, error) {
	settings := map[string]string{}
	for k, v := range config {
		settings[k] = v
	}
	if kind == model.KindLocal {
		settings["root"] = s.localRoot(id)
	}
	return s.factory(storage.Config{ID: id, Kind: kind, Settings: settings, Secrets: secret})
}

func validateDriveInput(in *DriveInput) error {
	if runeLen(strings.TrimSpace(in.Name)) < 1 || runeLen(in.Name) > 128 {
		return apperr.Validation("网盘名称必须为 1 到 128 字符")
	}
	if !isOneOf(in.Kind, model.StorageKinds...) {
		return apperr.Validation("网盘类型无效")
	}
	if in.OnPathChange == "" {
		in.OnPathChange = model.OnPathChangeMove
		if in.Kind == model.KindLanzou || in.Kind == model.KindPan123 {
			in.OnPathChange = model.OnPathChangeKeep
		}
	}
	if !isOneOf(in.OnPathChange, model.OnPathChangeModes...) || (in.Kind == model.KindLanzou || in.Kind == model.KindPan123) && in.OnPathChange != model.OnPathChangeKeep {
		return apperr.Validation("该网盘不支持移动，请选择保持原位")
	}
	if in.MaxFileBytes < 0 || in.ReadOrder < 0 || in.ReadOrder > 1000 {
		return apperr.Validation("文件上限或读取顺序无效")
	}
	if in.Kind == model.KindLanzou && in.MaxFileBytes == 0 {
		in.MaxFileBytes = 100 << 20
	}
	if in.Kind == model.KindLanzou {
		if strings.ContainsAny(in.RootPath, "/\\") {
			return apperr.Validation("蓝奏云根目录应为文件夹 ID")
		}
	} else if in.RootPath != "" {
		for _, seg := range strings.Split(in.RootPath, "/") {
			clean, err := cleanSyncSegment(seg)
			if err != nil || seg == "" || clean != seg {
				return apperr.Validation("根目录必须为安全的相对路径")
			}
		}
	}
	for i, e := range in.AllowedExts {
		e = strings.ToLower(strings.TrimSpace(e))
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if len(e) < 2 || strings.ContainsAny(e, "/\\*? ") {
			return apperr.Validation("扩展名格式无效")
		}
		in.AllowedExts[i] = e
	}
	return nil
}

// DriveInfo reports capabilities without making network requests.
func (s *Service) DriveInfo(ctx context.Context, d model.Drive) (storage.Capabilities, map[string]int64) {
	caps := storage.Capabilities{}
	if a, err := s.adapterFor(d); err == nil {
		caps = a.Capabilities()
	}
	stats := map[string]int64{}
	var rows []struct {
		Status string
		N      int64
		Bytes  int64
	}
	if s.db.WithContext(ctx).Model(&model.Replica{}).Select("status, COUNT(*) AS n, SUM(size_bytes) AS bytes").Where("drive_id = ?", d.ID).Group("status").Scan(&rows).Error == nil {
		for _, r := range rows {
			stats[r.Status] = r.N
			if r.Status == model.ReplicaReady || r.Status == model.ReplicaOrphaned {
				stats["size_bytes"] += r.Bytes
			}
		}
	}
	return caps, stats
}
