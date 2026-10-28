package service

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/auth"
	"github.com/nic/paper-collect/backend/internal/model"
	"gorm.io/gorm"
)

// TokenPair is returned by login and refresh. RefreshToken is shown once.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	User         model.User
}

// Login verifies credentials and opens a browser session. Failures are throttled
// per username and per client address; unknown users and wrong passwords look identical.
func (s *Service) Login(ctx context.Context, username, password, clientIP string) (TokenPair, error) {
	userKey := "user:" + strings.ToLower(username)
	ipKey := "ip:" + clientIP
	if blocked, _ := s.limiter.Blocked(userKey); blocked {
		return TokenPair{}, apperr.RateLimited("登录失败次数过多，请稍后再试")
	}
	if blocked, _ := s.limiter.Blocked(ipKey); blocked {
		return TokenPair{}, apperr.RateLimited("登录失败次数过多，请稍后再试")
	}
	fail := func() (TokenPair, error) {
		s.limiter.Failure(userKey)
		s.limiter.Failure(ipKey)
		return TokenPair{}, apperr.Unauthorized("用户名或口令错误")
	}
	if username == "" || len(password) == 0 || len(password) > auth.MaxPasswordBytes {
		auth.DummyVerify(password)
		return fail()
	}

	db := s.db.WithContext(ctx)
	var user model.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		auth.DummyVerify(password)
		return fail()
	}
	if !user.Enabled || !auth.VerifyPassword(password, user.PasswordHash) {
		return fail()
	}
	s.limiter.Success(userKey)

	refresh, refreshHash, err := auth.NewRefreshToken()
	if err != nil {
		return TokenPair{}, apperr.Internal(err)
	}
	now := s.now()
	sess := model.Session{
		ID:          newID(),
		UserID:      user.ID,
		RefreshHash: refreshHash,
		ExpiresAt:   now.Add(auth.RefreshTTL),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&sess).Error; err != nil {
			return err
		}
		return s.audit(tx, auth.Principal{UserID: user.ID}, "auth.login", user.ID)
	})
	if err != nil {
		return TokenPair{}, apperr.Internal(err)
	}
	return s.browserPair(user, sess.ID, refresh)
}

// Refresh rotates the refresh token. The old token stops working immediately.
// Presenting an already-rotated token revokes the session (reuse detection).
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	invalid := apperr.Unauthorized("刷新令牌无效或已过期")
	if refreshToken == "" || len(refreshToken) > 256 {
		return TokenPair{}, invalid
	}
	db := s.db.WithContext(ctx)
	h := auth.HashToken(refreshToken)
	now := s.now()

	var sess model.Session
	if err := db.Where("refresh_hash = ?", h).First(&sess).Error; err != nil {
		var reused model.Session
		if db.Where("prev_hash = ?", h).First(&reused).Error == nil {
			_ = db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&model.Session{}).Where("id = ?", reused.ID).
					Updates(map[string]any{"revoked": true, "updated_at": now}).Error; err != nil {
					return err
				}
				return s.audit(tx, auth.Principal{UserID: reused.UserID}, "auth.refresh_reuse", reused.ID)
			})
		}
		return TokenPair{}, invalid
	}
	if sess.Revoked || !now.Before(sess.ExpiresAt) {
		return TokenPair{}, invalid
	}
	var user model.User
	if err := db.Where("id = ?", sess.UserID).First(&user).Error; err != nil || !user.Enabled {
		return TokenPair{}, invalid
	}

	next, nextHash, err := auth.NewRefreshToken()
	if err != nil {
		return TokenPair{}, apperr.Internal(err)
	}
	res := db.Model(&model.Session{}).
		Where("id = ? AND refresh_hash = ? AND revoked = ?", sess.ID, h, false).
		Updates(map[string]any{"refresh_hash": nextHash, "prev_hash": h, "updated_at": now})
	if res.Error != nil {
		return TokenPair{}, apperr.Internal(res.Error)
	}
	if res.RowsAffected != 1 {
		return TokenPair{}, invalid
	}
	return s.browserPair(user, sess.ID, next)
}

// Logout revokes the caller's session and, when given, the session of refreshToken.
// Unknown tokens are not an error, so the response never reveals session existence.
func (s *Service) Logout(ctx context.Context, p auth.Principal, refreshToken string) error {
	db := s.db.WithContext(ctx)
	now := s.now()
	return db.Transaction(func(tx *gorm.DB) error {
		if p.SessionID != "" {
			if err := tx.Model(&model.Session{}).Where("id = ? AND user_id = ?", p.SessionID, p.UserID).
				Updates(map[string]any{"revoked": true, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if refreshToken != "" && len(refreshToken) <= 256 {
			if err := tx.Model(&model.Session{}).Where("refresh_hash = ? AND user_id = ?", auth.HashToken(refreshToken), p.UserID).
				Updates(map[string]any{"revoked": true, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, p, "auth.logout", p.UserID)
	})
}

// AgentToken exchanges a valid API key for a short-lived JWT bound to the key id.
func (s *Service) AgentToken(ctx context.Context, apiKey string) (string, error) {
	id, secret, err := auth.ParseAPIKey(apiKey)
	if err != nil {
		return "", apperr.Unauthorized("API Key 无效")
	}
	key, user, err := s.resolveAPIKey(ctx, id, secret)
	if err != nil {
		return "", err
	}
	token, err := s.signer.Sign(user.ID, "", key.ID)
	if err != nil {
		return "", apperr.Internal(err)
	}
	p := auth.Principal{UserID: user.ID, Role: user.Role, KeyID: key.ID}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.audit(tx, p, "auth.agent_token", key.ID)
	}); err != nil {
		return "", apperr.Internal(err)
	}
	return token, nil
}

// Authenticate resolves the caller from an access JWT.
// Every call re-checks user, session and key state, so revocation is immediate.
func (s *Service) Authenticate(ctx context.Context, bearer string) (auth.Principal, error) {
	unauth := apperr.Unauthorized("凭据无效或已过期")
	claims, err := s.signer.Parse(bearer)
	if err != nil {
		return auth.Principal{}, unauth
	}
	db := s.db.WithContext(ctx)

	if claims.KeyID != "" {
		key, err := s.activeAPIKey(ctx, claims.KeyID)
		if err != nil || key.UserID != claims.Subject {
			return auth.Principal{}, unauth
		}
		user, err := s.enabledUser(ctx, key.UserID)
		if err != nil {
			return auth.Principal{}, unauth
		}
		return auth.Principal{
			UserID:   user.ID,
			Username: user.Username,
			Role:     user.Role,
			KeyID:    key.ID,
			Scopes:   auth.EffectiveScopes(user.Role, decodeScopes(key.ScopesJSON)),
		}, nil
	}

	if claims.SessionID == "" {
		return auth.Principal{}, unauth
	}
	var sess model.Session
	if err := db.Where("id = ?", claims.SessionID).First(&sess).Error; err != nil {
		return auth.Principal{}, unauth
	}
	if sess.Revoked || !s.now().Before(sess.ExpiresAt) || sess.UserID != claims.Subject {
		return auth.Principal{}, unauth
	}
	user, err := s.enabledUser(ctx, sess.UserID)
	if err != nil {
		return auth.Principal{}, unauth
	}
	return auth.Principal{
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		SessionID: sess.ID,
		Scopes:    auth.EffectiveScopes(user.Role, nil),
	}, nil
}

// Me returns the owning user of the caller.
func (s *Service) Me(ctx context.Context, p auth.Principal) (model.User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).Where("id = ?", p.UserID).First(&user).Error; err != nil {
		return model.User{}, apperr.Unauthorized("凭据无效或已过期")
	}
	return user, nil
}

// resolveAPIKey checks revocation, expiry, the secret hash and the owner's state.
func (s *Service) resolveAPIKey(ctx context.Context, id, secret string) (model.APIKey, model.User, error) {
	unauth := apperr.Unauthorized("API Key 无效")
	key, err := s.activeAPIKey(ctx, id)
	if err != nil {
		return model.APIKey{}, model.User{}, unauth
	}
	if subtle.ConstantTimeCompare([]byte(auth.HashToken(secret)), []byte(key.SecretHash)) != 1 {
		return model.APIKey{}, model.User{}, unauth
	}
	user, err := s.enabledUser(ctx, key.UserID)
	if err != nil {
		return model.APIKey{}, model.User{}, unauth
	}
	return key, user, nil
}

func (s *Service) activeAPIKey(ctx context.Context, id string) (model.APIKey, error) {
	unauth := apperr.Unauthorized("API Key 无效")
	db := s.db.WithContext(ctx)
	var key model.APIKey
	if err := db.Where("id = ?", id).First(&key).Error; err != nil {
		return model.APIKey{}, unauth
	}
	if key.Revoked {
		return model.APIKey{}, unauth
	}
	if key.ExpiresAt != nil && !s.now().Before(*key.ExpiresAt) {
		return model.APIKey{}, unauth
	}
	return key, nil
}

func (s *Service) enabledUser(ctx context.Context, id string) (model.User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&user).Error; err != nil || !user.Enabled {
		return model.User{}, apperr.Unauthorized("凭据无效或已过期")
	}
	return user, nil
}

func (s *Service) browserPair(user model.User, sessionID, refresh string) (TokenPair, error) {
	access, err := s.signer.Sign(user.ID, sessionID, "")
	if err != nil {
		return TokenPair{}, apperr.Internal(err)
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(auth.AccessTTL / time.Second),
		User:         user,
	}, nil
}
