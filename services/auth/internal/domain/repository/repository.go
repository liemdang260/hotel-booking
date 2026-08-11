package repository

import (
	"context"
	"time"
	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)
type UserRepository interface{
	Create(context.Context,domain.User)error
	FindByEmail(context.Context,string)(domain.User,error)
	FindByID(context.Context,string)(domain.User,error)
}
type SessionRepository interface{
	Create(context.Context,domain.RefreshToken)error
	FindByHash(context.Context,string)(domain.RefreshToken,error)
	Rotate(context.Context,string,domain.RefreshToken,time.Time)error
	Revoke(context.Context,string,time.Time)error
	RevokeFamily(context.Context,string,time.Time)error
}
type TransactionManager interface{WithinTransaction(context.Context,func(context.Context,UserRepository,SessionRepository)error)error}
type PasswordHasher interface{Hash(string)(string,error);Verify(string,string)bool}
type TokenIssuer interface{
	IssueAccess(domain.AccessClaims)(string,error)
	IssueRefresh()(plain,hash string,err error)
	PublicJWKS()([]byte,error)
}
type IDGenerator interface{NewID()string}
