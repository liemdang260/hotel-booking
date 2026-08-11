package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
	"github.com/liemdang260/hotel-booking/services/auth/internal/domain/repository"
)

type Tokens struct{AccessToken,RefreshToken string;AccessExpiresAt,RefreshExpiresAt time.Time}
type Service struct{
	users repository.UserRepository;sessions repository.SessionRepository
	hasher repository.PasswordHasher;issuer repository.TokenIssuer;ids repository.IDGenerator
	now func()time.Time;accessTTL,refreshTTL time.Duration;issuerName,audience string
}
func NewService(u repository.UserRepository,s repository.SessionRepository,h repository.PasswordHasher,t repository.TokenIssuer,ids repository.IDGenerator,issuer,audience string)*Service{
	return &Service{users:u,sessions:s,hasher:h,issuer:t,ids:ids,now:time.Now,accessTTL:15*time.Minute,refreshTTL:30*24*time.Hour,issuerName:issuer,audience:audience}
}
func(s *Service)Register(ctx context.Context,email,password string)(domain.User,error){
	email=strings.ToLower(strings.TrimSpace(email));if email==""||len(password)<12{return domain.User{},domain.ErrInvalidCredentials}
	if _,err:=s.users.FindByEmail(ctx,email);err==nil{return domain.User{},domain.ErrEmailExists}
	hash,err:=s.hasher.Hash(password);if err!=nil{return domain.User{},err}
	now:=s.now().UTC();u:=domain.User{ID:s.ids.NewID(),Email:email,PasswordHash:hash,Status:domain.UserActive,CreatedAt:now,UpdatedAt:now}
	if err=u.Validate();err!=nil{return domain.User{},err};if err=s.users.Create(ctx,u);err!=nil{return domain.User{},err};return u,nil
}
func(s *Service)Login(ctx context.Context,email,password string)(Tokens,error){
	u,err:=s.users.FindByEmail(ctx,strings.ToLower(strings.TrimSpace(email)))
	if err!=nil||u.Status!=domain.UserActive||!s.hasher.Verify(password,u.PasswordHash){return Tokens{},domain.ErrInvalidCredentials}
	return s.newSession(ctx,u,"")
}
func(s *Service)newSession(ctx context.Context,u domain.User,family string)(Tokens,error){
	now:=s.now().UTC();if family==""{family=s.ids.NewID()}
	plain,hash,err:=s.issuer.IssueRefresh();if err!=nil{return Tokens{},err}
	r:=domain.RefreshToken{ID:s.ids.NewID(),UserID:u.ID,FamilyID:family,TokenHash:hash,ExpiresAt:now.Add(s.refreshTTL),CreatedAt:now}
	if err=s.sessions.Create(ctx,r);err!=nil{return Tokens{},err}
	accessExp:=now.Add(s.accessTTL);access,err:=s.issuer.IssueAccess(domain.AccessClaims{Subject:u.ID,Issuer:s.issuerName,Audience:s.audience,TokenID:s.ids.NewID(),Roles:[]string{"customer"},IssuedAt:now,ExpiresAt:accessExp})
	if err!=nil{return Tokens{},err};return Tokens{AccessToken:access,RefreshToken:plain,AccessExpiresAt:accessExp,RefreshExpiresAt:r.ExpiresAt},nil
}
func(s *Service)Refresh(ctx context.Context,plain string)(Tokens,error){
	hash:=HashOpaqueToken(plain);old,err:=s.sessions.FindByHash(ctx,hash);if err!=nil{return Tokens{},domain.ErrInvalidRefreshToken}
	now:=s.now().UTC()
	if old.RevokedAt!=nil||old.RotatedToID!=""{
		_ = s.sessions.RevokeFamily(ctx,old.FamilyID,now);return Tokens{},domain.ErrRefreshTokenReuse
	}
	if !old.Active(now){return Tokens{},domain.ErrInvalidRefreshToken}
	u,err:=s.users.FindByID(ctx,old.UserID);if err!=nil||u.Status!=domain.UserActive{return Tokens{},domain.ErrInvalidRefreshToken}
	nextPlain,nextHash,err:=s.issuer.IssueRefresh();if err!=nil{return Tokens{},err}
	next:=domain.RefreshToken{ID:s.ids.NewID(),UserID:u.ID,FamilyID:old.FamilyID,TokenHash:nextHash,RotatedFromID:old.ID,ExpiresAt:now.Add(s.refreshTTL),CreatedAt:now}
	if err=s.sessions.Rotate(ctx,old.ID,next,now);err!=nil{
		if errors.Is(err,domain.ErrRefreshTokenReuse){_ = s.sessions.RevokeFamily(ctx,old.FamilyID,now)}
		return Tokens{},err
	}
	exp:=now.Add(s.accessTTL);access,err:=s.issuer.IssueAccess(domain.AccessClaims{Subject:u.ID,Issuer:s.issuerName,Audience:s.audience,TokenID:s.ids.NewID(),Roles:[]string{"customer"},IssuedAt:now,ExpiresAt:exp})
	if err!=nil{return Tokens{},err};return Tokens{AccessToken:access,RefreshToken:nextPlain,AccessExpiresAt:exp,RefreshExpiresAt:next.ExpiresAt},nil
}
func(s *Service)Logout(ctx context.Context,plain string)error{
	hash:=HashOpaqueToken(plain);r,err:=s.sessions.FindByHash(ctx,hash);if err!=nil{return nil};return s.sessions.Revoke(ctx,r.ID,s.now().UTC())
}
func HashOpaqueToken(plain string)string{return hashOpaqueToken(plain)}
