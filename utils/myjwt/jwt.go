package myjwt

import (
	"GoAI/config"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Token 结构
type Claims struct {
	ID                   int64  `json:"id"`       // 用户id
	Username             string `json:"username"` // 用户名
	jwt.RegisteredClaims        // 包括过期时间 (exp)、发行者 (iss)、主题 (sub)、发行时间 (iat)。
}

// GenerateToken 生成Token
func GenerateToken(id int64, username string) (string, error) {
	claims := Claims{
		ID:       id,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(config.GetConfig().ExpireDuration) * time.Hour)),
			Issuer:    config.GetConfig().Issuer,
			Subject:   config.GetConfig().Subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// 生成Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.GetConfig().Key))
}

// ParseToken 解析Token
func ParseToken(token string) (string, bool) {
	claims := new(Claims)
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(config.GetConfig().Key), nil
	})
	if !t.Valid || err != nil || claims == nil {
		return "", false
	}
	return claims.Username, true
}
