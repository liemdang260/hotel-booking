package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
)

type Store struct{db *sql.DB}
func NewStore(db *sql.DB)*Store{return &Store{db:db}}
func(s *Store)Create(ctx context.Context,u domain.User)error{
	_,err:=s.db.ExecContext(ctx,`INSERT INTO auth_users(id,email,password_hash,status,created_at,updated_at)VALUES($1,lower($2),$3,$4,$5,$6)`,u.ID,u.Email,u.PasswordHash,u.Status,u.CreatedAt,u.UpdatedAt)
	if err!=nil&&strings.Contains(err.Error(),"auth_users_email_ci_unique"){return domain.ErrEmailExists};return err
}
func(s *Store)FindByEmail(ctx context.Context,email string)(domain.User,error){
	var u domain.User;err:=s.db.QueryRowContext(ctx,`SELECT id::text,email,password_hash,status,created_at,updated_at FROM auth_users WHERE lower(email)=lower($1)`,email).Scan(&u.ID,&u.Email,&u.PasswordHash,&u.Status,&u.CreatedAt,&u.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){return u,domain.ErrInvalidCredentials};return u,err
}
func(s *Store)FindByID(ctx context.Context,id string)(domain.User,error){
	var u domain.User;err:=s.db.QueryRowContext(ctx,`SELECT id::text,email,password_hash,status,created_at,updated_at FROM auth_users WHERE id=$1`,id).Scan(&u.ID,&u.Email,&u.PasswordHash,&u.Status,&u.CreatedAt,&u.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){return u,domain.ErrInvalidCredentials};return u,err
}
func(s *Store)CreateToken(ctx context.Context,r domain.RefreshToken)error{
	_,err:=s.db.ExecContext(ctx,`INSERT INTO auth_refresh_tokens(id,user_id,family_id,token_hash,expires_at,revoked_at,rotated_from,rotated_to,created_at)
VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,$9)`,r.ID,r.UserID,r.FamilyID,r.TokenHash,r.ExpiresAt,r.RevokedAt,r.RotatedFromID,r.RotatedToID,r.CreatedAt);return err
}
func(s *Store)FindByHash(ctx context.Context,h string)(domain.RefreshToken,error){
	var r domain.RefreshToken
	err:=s.db.QueryRowContext(ctx,`SELECT id::text,user_id::text,family_id::text,token_hash,expires_at,revoked_at,COALESCE(rotated_from::text,''),COALESCE(rotated_to::text,''),created_at FROM auth_refresh_tokens WHERE token_hash=$1`,h).Scan(&r.ID,&r.UserID,&r.FamilyID,&r.TokenHash,&r.ExpiresAt,&r.RevokedAt,&r.RotatedFromID,&r.RotatedToID,&r.CreatedAt)
	if errors.Is(err,sql.ErrNoRows){return r,domain.ErrInvalidRefreshToken};return r,err
}
func(s *Store)Rotate(ctx context.Context,oldID string,next domain.RefreshToken,now time.Time)error{
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer func(){_=tx.Rollback()}()
	res,err:=tx.ExecContext(ctx,`UPDATE auth_refresh_tokens SET revoked_at=$2,rotated_to=$3 WHERE id=$1 AND revoked_at IS NULL AND rotated_to IS NULL AND expires_at>$2`,oldID,now,next.ID)
	if err!=nil{return err};n,err:=res.RowsAffected();if err!=nil{return err};if n!=1{return domain.ErrRefreshTokenReuse}
	_,err=tx.ExecContext(ctx,`INSERT INTO auth_refresh_tokens(id,user_id,family_id,token_hash,expires_at,rotated_from,created_at)VALUES($1,$2,$3,$4,$5,$6,$7)`,next.ID,next.UserID,next.FamilyID,next.TokenHash,next.ExpiresAt,oldID,next.CreatedAt)
	if err!=nil{return err};return tx.Commit()
}
func(s *Store)Revoke(ctx context.Context,id string,now time.Time)error{
	_,err:=s.db.ExecContext(ctx,`UPDATE auth_refresh_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1`,id,now);return err
}
func(s *Store)RevokeFamily(ctx context.Context,family string,now time.Time)error{
	_,err:=s.db.ExecContext(ctx,`UPDATE auth_refresh_tokens SET revoked_at=COALESCE(revoked_at,$2) WHERE family_id=$1`,family,now);return err
}

type UserRepository struct{*Store}
type SessionRepository struct{*Store}
func NewUserRepository(db *sql.DB)*UserRepository{return &UserRepository{NewStore(db)}}
func NewSessionRepository(db *sql.DB)*SessionRepository{return &SessionRepository{NewStore(db)}}
func(r *SessionRepository)Create(ctx context.Context,t domain.RefreshToken)error{return r.CreateToken(ctx,t)}
