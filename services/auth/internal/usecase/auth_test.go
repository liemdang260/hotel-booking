package usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)
type userMem struct{byEmail map[string]domain.User}
func(u *userMem)Create(_ context.Context,v domain.User)error{if u.byEmail==nil{u.byEmail=map[string]domain.User{}};u.byEmail[v.Email]=v;return nil}
func(u *userMem)FindByEmail(_ context.Context,e string)(domain.User,error){v,ok:=u.byEmail[e];if !ok{return v,errors.New("not found")};return v,nil}
func(u *userMem)FindByID(_ context.Context,id string)(domain.User,error){for _,v:=range u.byEmail{if v.ID==id{return v,nil}};return domain.User{},errors.New("not found")}
type sessionMem struct{byHash map[string]domain.RefreshToken;revokedFamilies map[string]bool}
func(s *sessionMem)Create(_ context.Context,r domain.RefreshToken)error{if s.byHash==nil{s.byHash=map[string]domain.RefreshToken{}};s.byHash[r.TokenHash]=r;return nil}
func(s *sessionMem)FindByHash(_ context.Context,h string)(domain.RefreshToken,error){r,ok:=s.byHash[h];if !ok{return r,errors.New("not found")};return r,nil}
func(s *sessionMem)Rotate(_ context.Context,id string,next domain.RefreshToken,now time.Time)error{for h,r:=range s.byHash{if r.ID==id{if r.RevokedAt!=nil||r.RotatedToID!=""{return domain.ErrRefreshTokenReuse};r.RevokedAt=&now;r.RotatedToID=next.ID;s.byHash[h]=r;s.byHash[next.TokenHash]=next;return nil}};return errors.New("not found")}
func(s *sessionMem)Revoke(_ context.Context,id string,now time.Time)error{for h,r:=range s.byHash{if r.ID==id{r.RevokedAt=&now;s.byHash[h]=r}};return nil}
func(s *sessionMem)RevokeFamily(_ context.Context,f string,now time.Time)error{if s.revokedFamilies==nil{s.revokedFamilies=map[string]bool{}};s.revokedFamilies[f]=true;for h,r:=range s.byHash{if r.FamilyID==f&&r.RevokedAt==nil{r.RevokedAt=&now;s.byHash[h]=r}};return nil}
type hashFake struct{}
func(hashFake)Hash(p string)(string,error){return "hash:"+p,nil};func(hashFake)Verify(p,h string)bool{return h=="hash:"+p}
type issuerFake struct{n int}
func(i *issuerFake)IssueRefresh()(string,string,error){i.n++;p:=fmt.Sprintf("refresh-%d",i.n);return p,HashOpaqueToken(p),nil}
func(i *issuerFake)IssueAccess(domain.AccessClaims)(string,error){i.n++;return fmt.Sprintf("access-%d",i.n),nil}
func(i *issuerFake)PublicJWKS()([]byte,error){return []byte(`{"keys":[]}`),nil}
type idsFake struct{n int};func(i *idsFake)NewID()string{i.n++;return fmt.Sprintf("00000000-0000-0000-0000-%012d",i.n)}
func newAuthFixture(now time.Time)(*Service,*sessionMem){
	u:=&userMem{byEmail:map[string]domain.User{"user@example.com":{ID:"user-1",Email:"user@example.com",PasswordHash:"hash:long-enough-password",Status:domain.UserActive}}};ss:=&sessionMem{};svc:=NewService(u,ss,hashFake{},&issuerFake{},&idsFake{},"iss","aud");svc.now=func()time.Time{return now};return svc,ss
}
func TestRefreshRotatesAndReusedAncestorRevokesFamily(t *testing.T){
	now:=time.Date(2026,8,11,10,0,0,0,time.UTC);svc,sessions:=newAuthFixture(now)
	first,err:=svc.Login(context.Background(),"user@example.com","long-enough-password");if err!=nil{t.Fatal(err)}
	second,err:=svc.Refresh(context.Background(),first.RefreshToken);if err!=nil{t.Fatal(err)};if second.RefreshToken==first.RefreshToken{t.Fatal("refresh not rotated")}
	if _,err=svc.Refresh(context.Background(),first.RefreshToken);!errors.Is(err,domain.ErrRefreshTokenReuse){t.Fatalf("reuse err=%v",err)}
	for _,r:=range sessions.byHash{if r.RevokedAt==nil{t.Fatalf("family member active after reuse: %+v",r)}}
}
func TestLogoutRevokesOpaqueRefreshToken(t *testing.T){
	svc,sessions:=newAuthFixture(time.Now());tokens,err:=svc.Login(context.Background(),"user@example.com","long-enough-password");if err!=nil{t.Fatal(err)}
	if err=svc.Logout(context.Background(),tokens.RefreshToken);err!=nil{t.Fatal(err)};r,_:=sessions.FindByHash(context.Background(),HashOpaqueToken(tokens.RefreshToken));if r.RevokedAt==nil{t.Fatal("not revoked")}
}
func TestLoginUsesGenericInvalidCredentials(t *testing.T){
	svc,_:=newAuthFixture(time.Now());for _,tc:=range []struct{email,password string}{{"missing@example.com","long-enough-password"},{"user@example.com","wrong-password"}}{if _,err:=svc.Login(context.Background(),tc.email,tc.password);!errors.Is(err,domain.ErrInvalidCredentials){t.Fatalf("err=%v",err)}}
}
