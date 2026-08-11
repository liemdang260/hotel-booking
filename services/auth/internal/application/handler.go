package application

import (
	"context"
	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
	"github.com/liemdang260/hotel-booking/services/auth/internal/usecase"
)
type AuthUsecases interface{
	Register(context.Context,string,string)(domain.User,error)
	Login(context.Context,string,string)(usecase.Tokens,error)
	Refresh(context.Context,string)(usecase.Tokens,error)
	Logout(context.Context,string)error
}
type Handler struct{auth AuthUsecases}
func NewHandler(a AuthUsecases)*Handler{return &Handler{auth:a}}
type Credentials struct{Email,Password string}
func(h *Handler)Register(ctx context.Context,r Credentials)(domain.User,error){return h.auth.Register(ctx,r.Email,r.Password)}
func(h *Handler)Login(ctx context.Context,r Credentials)(usecase.Tokens,error){return h.auth.Login(ctx,r.Email,r.Password)}
func(h *Handler)Refresh(ctx context.Context,token string)(usecase.Tokens,error){return h.auth.Refresh(ctx,token)}
func(h *Handler)Logout(ctx context.Context,token string)error{return h.auth.Logout(ctx,token)}
