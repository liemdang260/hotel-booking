package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liemdang260/hotel-booking/services/auth/internal/domain"
	"github.com/liemdang260/hotel-booking/services/auth/internal/usecase"
)

type authUsecasesStub struct {
	user domain.User
}

func (s authUsecasesStub) Register(context.Context, string, string) (domain.User, error) {
	return s.user, nil
}

func (authUsecasesStub) Login(context.Context, string, string) (usecase.Tokens, error) {
	return usecase.Tokens{}, nil
}

func (authUsecasesStub) Refresh(context.Context, string) (usecase.Tokens, error) {
	return usecase.Tokens{}, nil
}

func (authUsecasesStub) Logout(context.Context, string) error { return nil }

func TestRegisterResponseDoesNotExposePasswordHash(t *testing.T) {
	createdAt := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	handler := NewHandler(authUsecasesStub{user: domain.User{
		ID:           "user-1",
		Email:        "user@example.com",
		PasswordHash: "pbkdf2-secret-hash",
		Status:       domain.UserActive,
		CreatedAt:    createdAt,
	}})

	result, err := handler.Register(context.Background(), Credentials{
		Email:    "user@example.com",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "user-1" || result.Email != "user@example.com" || result.CreatedAt != createdAt {
		t.Fatalf("unexpected response: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PasswordHash") || strings.Contains(string(encoded), "pbkdf2-secret-hash") {
		t.Fatalf("credential hash leaked in response: %s", encoded)
	}
}
