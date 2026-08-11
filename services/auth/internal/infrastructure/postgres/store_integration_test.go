//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)
func openAuthDB(t *testing.T)*sql.DB{t.Helper();dsn:=os.Getenv("AUTH_TEST_DATABASE_URL");if dsn==""{t.Skip("AUTH_TEST_DATABASE_URL not set")};db,err:=sql.Open("postgres",dsn);if err!=nil{t.Fatal(err)};t.Cleanup(func(){_=db.Close()});_,err=db.Exec(`TRUNCATE auth_refresh_tokens,auth_users CASCADE`);if err!=nil{t.Fatal(err)};return db}
func TestIntegrationRefreshRotationAllowsOneConcurrentWinner(t *testing.T){
	db:=openAuthDB(t);ctx:=context.Background();now:=time.Now().UTC();users:=NewUserRepository(db);sessions:=NewSessionRepository(db)
	u:=domain.User{ID:"00000000-0000-0000-0000-000000001001",Email:"u@example.com",PasswordHash:"pbkdf2-sha256$210000$salt$hash",Status:domain.UserActive,CreatedAt:now,UpdatedAt:now};if err:=users.Create(ctx,u);err!=nil{t.Fatal(err)}
	old:=domain.RefreshToken{ID:"00000000-0000-0000-0000-000000001002",UserID:u.ID,FamilyID:"00000000-0000-0000-0000-000000001003",TokenHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",ExpiresAt:now.Add(time.Hour),CreatedAt:now};if err:=sessions.Create(ctx,old);err!=nil{t.Fatal(err)}
	var winners atomic.Int32;var wg sync.WaitGroup
	for i:=0;i<2;i++{wg.Add(1);go func(n int){defer wg.Done();id:="00000000-0000-0000-0000-000000001004";hash:="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";if n==1{id="00000000-0000-0000-0000-000000001005";hash="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"};next:=domain.RefreshToken{ID:id,UserID:u.ID,FamilyID:old.FamilyID,TokenHash:hash,RotatedFromID:old.ID,ExpiresAt:now.Add(time.Hour),CreatedAt:now};if sessions.Rotate(ctx,old.ID,next,now)==nil{winners.Add(1)}}(i)}
	wg.Wait();if winners.Load()!=1{t.Fatalf("rotation winners=%d",winners.Load())}
	var children int;if err:=db.QueryRow(`SELECT count(*) FROM auth_refresh_tokens WHERE rotated_from=$1`,old.ID).Scan(&children);err!=nil{t.Fatal(err)};if children!=1{t.Fatalf("children=%d",children)}
}
func TestIntegrationReuseRevokesWholeFamily(t *testing.T){
	db:=openAuthDB(t);ctx:=context.Background();now:=time.Now().UTC();users:=NewUserRepository(db);sessions:=NewSessionRepository(db)
	u:=domain.User{ID:"00000000-0000-0000-0000-000000002001",Email:"r@example.com",PasswordHash:"x",Status:domain.UserActive,CreatedAt:now,UpdatedAt:now};if err:=users.Create(ctx,u);err!=nil{t.Fatal(err)}
	for i,h:=range []string{"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}{id:="00000000-0000-0000-0000-000000002002";if i==1{id="00000000-0000-0000-0000-000000002003"};if err:=sessions.Create(ctx,domain.RefreshToken{ID:id,UserID:u.ID,FamilyID:"00000000-0000-0000-0000-000000002004",TokenHash:h,ExpiresAt:now.Add(time.Hour),CreatedAt:now});err!=nil{t.Fatal(err)}}
	if err:=sessions.RevokeFamily(ctx,"00000000-0000-0000-0000-000000002004",now);err!=nil{t.Fatal(err)}
	var active int;if err:=db.QueryRow(`SELECT count(*) FROM auth_refresh_tokens WHERE family_id='00000000-0000-0000-0000-000000002004' AND revoked_at IS NULL`).Scan(&active);err!=nil{t.Fatal(err)};if active!=0{t.Fatalf("active family tokens=%d",active)}
}
