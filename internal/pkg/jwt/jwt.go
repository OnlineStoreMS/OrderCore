package jwt

import (
	"errors"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	UserID      uint64   `json:"uid"`
	CompanyID   uint64   `json:"cid"`
	TenantID    uint64   `json:"tid"`
	Email       string   `json:"email"`
	DisplayName string   `json:"name"`
	Permissions []string `json:"perms"`
	IsPlatform  bool     `json:"platform"`
	jwtlib.RegisteredClaims
}

type Manager struct {
	secret []byte
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

func (m *Manager) ParseAccess(tokenStr string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(tokenStr, &Claims{}, func(t *jwtlib.Token) (interface{}, error) {
		if t.Method != jwtlib.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// IssueServiceToken 签发定时任务用的短时服务令牌（与 UserCore 同密钥时可调用下游）。
func (m *Manager) IssueServiceToken(tenantID uint64, ttl time.Duration) (string, error) {
	if m == nil || len(m.secret) == 0 {
		return "", errors.New("jwt secret not configured")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	now := time.Now()
	claims := Claims{
		UserID:      0,
		TenantID:    tenantID,
		DisplayName: "ordercore-scheduler",
		Permissions: []string{"order:read", "order:write"},
		RegisteredClaims: jwtlib.RegisteredClaims{
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(ttl)),
			Issuer:    "ordercore",
		},
	}
	t := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	return t.SignedString(m.secret)
}
