package jwt

import (
	"errors"

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
