package infrastructure

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)

type PasswordHasher struct{Iterations int;SaltBytes int}
func NewPasswordHasher()*PasswordHasher{return &PasswordHasher{Iterations:210000,SaltBytes:16}}
func(h *PasswordHasher)Hash(password string)(string,error){
	if len(password)<12{return "",domain.ErrInvalidCredentials};salt:=make([]byte,h.SaltBytes);if _,err:=rand.Read(salt);err!=nil{return "",err}
	dk:=pbkdf2SHA256([]byte(password),salt,h.Iterations,32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",h.Iterations,base64.RawStdEncoding.EncodeToString(salt),base64.RawStdEncoding.EncodeToString(dk)),nil
}
func(h *PasswordHasher)Verify(password,encoded string)bool{
	parts:=strings.Split(encoded,"$");if len(parts)!=4||parts[0]!="pbkdf2-sha256"{return false}
	n,err:=strconv.Atoi(parts[1]);if err!=nil||n<210000{return false};salt,err:=base64.RawStdEncoding.DecodeString(parts[2]);if err!=nil{return false};want,err:=base64.RawStdEncoding.DecodeString(parts[3]);if err!=nil{return false}
	got:=pbkdf2SHA256([]byte(password),salt,n,len(want));return subtle.ConstantTimeCompare(got,want)==1
}
func pbkdf2SHA256(password,salt []byte,iterations,keyLen int)[]byte{
	out:=make([]byte,0,keyLen);var block uint32=1
	for len(out)<keyLen{mac:=hmac.New(sha256.New,password);_,_=mac.Write(salt);var b [4]byte;binary.BigEndian.PutUint32(b[:],block);_,_=mac.Write(b[:]);u:=mac.Sum(nil);t:=append([]byte(nil),u...)
		for i:=1;i<iterations;i++{mac=hmac.New(sha256.New,password);_,_=mac.Write(u);u=mac.Sum(nil);for j:=range t{t[j]^=u[j]}}
		out=append(out,t...);block++}
	return out[:keyLen]
}

type SigningKey struct{ID string;Private *rsa.PrivateKey}
type TokenManager struct{Active SigningKey;Verification map[string]*rsa.PublicKey}
func NewTokenManager(active SigningKey,previous ...SigningKey)*TokenManager{
	keys:=map[string]*rsa.PublicKey{active.ID:&active.Private.PublicKey};for _,k:=range previous{keys[k.ID]=&k.Private.PublicKey};return &TokenManager{Active:active,Verification:keys}
}
func(m *TokenManager)IssueRefresh()(string,string,error){
	raw:=make([]byte,32);if _,err:=rand.Read(raw);err!=nil{return "","",err};plain:=base64.RawURLEncoding.EncodeToString(raw);sum:=sha256.Sum256([]byte(plain));return plain,fmt.Sprintf("%x",sum[:]),nil
}
func(m *TokenManager)IssueAccess(c domain.AccessClaims)(string,error){
	if m.Active.ID==""||m.Active.Private==nil||c.Subject==""||c.Issuer==""||c.Audience==""||!c.ExpiresAt.After(c.IssuedAt){return "",domain.ErrInvalidAccessToken}
	header,_:=json.Marshal(map[string]any{"alg":"RS256","typ":"JWT","kid":m.Active.ID})
	claims,_:=json.Marshal(map[string]any{"sub":c.Subject,"iss":c.Issuer,"aud":c.Audience,"jti":c.TokenID,"roles":c.Roles,"iat":c.IssuedAt.Unix(),"exp":c.ExpiresAt.Unix()})
	input:=base64.RawURLEncoding.EncodeToString(header)+"."+base64.RawURLEncoding.EncodeToString(claims);digest:=sha256.Sum256([]byte(input))
	sig,err:=rsa.SignPKCS1v15(rand.Reader,m.Active.Private,crypto.SHA256,digest[:]);if err!=nil{return "",err};return input+"."+base64.RawURLEncoding.EncodeToString(sig),nil
}
func(m *TokenManager)VerifyAccess(token,issuer,audience string,now time.Time)(domain.Principal,error){
	parts:=strings.Split(token,".");if len(parts)!=3{return domain.Principal{},domain.ErrInvalidAccessToken}
	hb,err:=base64.RawURLEncoding.DecodeString(parts[0]);if err!=nil{return domain.Principal{},domain.ErrInvalidAccessToken};var h struct{Alg,Typ,Kid string};if json.Unmarshal(hb,&h)!=nil||h.Alg!="RS256"||h.Typ!="JWT"||h.Kid==""{return domain.Principal{},domain.ErrInvalidAccessToken}
	key,ok:=m.Verification[h.Kid];if !ok{return domain.Principal{},domain.ErrInvalidAccessToken}
	sig,err:=base64.RawURLEncoding.DecodeString(parts[2]);if err!=nil{return domain.Principal{},domain.ErrInvalidAccessToken};digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]));if rsa.VerifyPKCS1v15(key,crypto.SHA256,digest[:],sig)!=nil{return domain.Principal{},domain.ErrInvalidAccessToken}
	cb,err:=base64.RawURLEncoding.DecodeString(parts[1]);if err!=nil{return domain.Principal{},domain.ErrInvalidAccessToken}
	var c struct{Sub,Iss,Aud string;Roles []string;Iat,Exp int64};if json.Unmarshal(cb,&c)!=nil||c.Sub==""||c.Iss!=issuer||c.Aud!=audience||c.Exp<=now.Unix()||c.Iat>now.Add(time.Minute).Unix(){return domain.Principal{},domain.ErrInvalidAccessToken}
	return domain.Principal{UserID:c.Sub,Roles:c.Roles},nil
}
func(m *TokenManager)PublicJWKS()([]byte,error){
	keys:=make([]map[string]string,0,len(m.Verification));for kid,k:=range m.Verification{e:=make([]byte,4);binary.BigEndian.PutUint32(e,uint32(k.E));e=bytesTrimLeftZero(e);keys=append(keys,map[string]string{"kty":"RSA","use":"sig","alg":"RS256","kid":kid,"n":base64.RawURLEncoding.EncodeToString(k.N.Bytes()),"e":base64.RawURLEncoding.EncodeToString(e)})};return json.Marshal(map[string]any{"keys":keys})
}
func bytesTrimLeftZero(b []byte)[]byte{for len(b)>1&&b[0]==0{b=b[1:]};return b}
func PublicKeyFromJWK(n,e string)(*rsa.PublicKey,error){nb,err:=base64.RawURLEncoding.DecodeString(n);if err!=nil{return nil,err};eb,err:=base64.RawURLEncoding.DecodeString(e);if err!=nil{return nil,err};v:=0;for _,b:=range eb{v=v<<8+int(b)};if v<3{return nil,errors.New("invalid exponent")};return &rsa.PublicKey{N:new(big.Int).SetBytes(nb),E:v},nil}
