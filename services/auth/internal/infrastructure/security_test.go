package infrastructure

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)
func TestPasswordHasherNeverStoresPlaintextAndVerifies(t *testing.T){
	h:=NewPasswordHasher();h.Iterations=210000
	encoded,err:=h.Hash("correct horse battery staple");if err!=nil{t.Fatal(err)}
	if strings.Contains(encoded,"correct horse"){t.Fatal("plaintext leaked")}
	if !h.Verify("correct horse battery staple",encoded)||h.Verify("wrong password",encoded){t.Fatal("password verification mismatch")}
}
func TestJWTValidationAndKeyRotationOverlap(t *testing.T){
	oldKey,_:=rsa.GenerateKey(rand.Reader,1024);newKey,_:=rsa.GenerateKey(rand.Reader,1024)
	old:=NewTokenManager(SigningKey{ID:"old",Private:oldKey});now:=time.Date(2026,8,11,10,0,0,0,time.UTC)
	oldToken,err:=old.IssueAccess(domain.AccessClaims{Subject:"user-1",Issuer:"hotel-booking-auth",Audience:"hotel-booking-api",TokenID:"j1",IssuedAt:now,ExpiresAt:now.Add(15*time.Minute)});if err!=nil{t.Fatal(err)}
	rotated:=NewTokenManager(SigningKey{ID:"new",Private:newKey},SigningKey{ID:"old",Private:oldKey})
	if p,err:=rotated.VerifyAccess(oldToken,"hotel-booking-auth","hotel-booking-api",now);err!=nil||p.UserID!="user-1"{t.Fatalf("overlap verify p=%+v err=%v",p,err)}
	newToken,_:=rotated.IssueAccess(domain.AccessClaims{Subject:"user-1",Issuer:"hotel-booking-auth",Audience:"hotel-booking-api",TokenID:"j2",IssuedAt:now,ExpiresAt:now.Add(time.Minute)})
	parts:=strings.Split(newToken,".");header,_:=base64.RawURLEncoding.DecodeString(parts[0]);var h map[string]any;_ = json.Unmarshal(header,&h);if h["kid"]!="new"{t.Fatalf("kid=%v",h["kid"])}
}
func TestJWTRejectsSignatureIssuerAudienceExpiryAndUnknownKid(t *testing.T){
	k,_:=rsa.GenerateKey(rand.Reader,1024);other,_:=rsa.GenerateKey(rand.Reader,1024);now:=time.Now().UTC()
	m:=NewTokenManager(SigningKey{ID:"k1",Private:k});token,_:=m.IssueAccess(domain.AccessClaims{Subject:"u",Issuer:"iss",Audience:"aud",IssuedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Minute)})
	cases:=[]struct{name,token,iss,aud string;at time.Time}{
		{"issuer",token,"wrong","aud",now},{"audience",token,"iss","wrong",now},{"expired",token,"iss","aud",now.Add(2*time.Minute)},
	}
	otherManager:=NewTokenManager(SigningKey{ID:"other",Private:other});cases=append(cases,struct{name,token,iss,aud string;at time.Time}{"unknown kid",token,"iss","aud",now})
	for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){verifier:=m;if tc.name=="unknown kid"{verifier=otherManager};if _,err:=verifier.VerifyAccess(tc.token,tc.iss,tc.aud,tc.at);err==nil{t.Fatal("accepted invalid token")}})}
	parts:=strings.Split(token,".");parts[2]=base64.RawURLEncoding.EncodeToString([]byte("bad"));if _,err:=m.VerifyAccess(strings.Join(parts,"."),"iss","aud",now);err==nil{t.Fatal("accepted invalid signature")}
}
