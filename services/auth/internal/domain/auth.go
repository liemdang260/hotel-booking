package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidCredentials=errors.New("invalid credentials")
	ErrEmailExists=errors.New("email already registered")
	ErrInvalidRefreshToken=errors.New("invalid refresh token")
	ErrRefreshTokenReuse=errors.New("refresh token reuse detected")
	ErrSessionRevoked=errors.New("session revoked")
	ErrInvalidAccessToken=errors.New("invalid access token")
)
type UserStatus string
const(UserActive UserStatus="ACTIVE";UserDisabled UserStatus="DISABLED")
type User struct{ID,Email,PasswordHash string;Status UserStatus;CreatedAt,UpdatedAt time.Time}
func(u User)Validate()error{if u.ID==""||!strings.Contains(u.Email,"@")||u.PasswordHash==""||u.Status==""{return ErrInvalidCredentials};return nil}
type RefreshToken struct{
	ID,UserID,FamilyID,TokenHash,RotatedFromID,RotatedToID string
	ExpiresAt time.Time;RevokedAt *time.Time;CreatedAt time.Time
}
func(t RefreshToken)Active(now time.Time)bool{return t.RevokedAt==nil&&now.Before(t.ExpiresAt)&&t.RotatedToID==""}
type Principal struct{UserID string;Roles []string}
type AccessClaims struct{Subject,Issuer,Audience,TokenID string;Roles []string;IssuedAt,ExpiresAt time.Time}
