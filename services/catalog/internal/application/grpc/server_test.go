package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain"
	"github.com/liemdang260/hotel-booking/services/catalog/internal/domain/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapErrorPreservesStableCatalogCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "canceled", err: context.Canceled, code: codes.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, code: codes.DeadlineExceeded},
		{name: "invalid id", err: domain.ErrInvalidCatalogID, code: codes.InvalidArgument},
		{name: "invalid search", err: domain.ErrInvalidSearch, code: codes.InvalidArgument},
		{name: "not found", err: repository.ErrNotFound, code: codes.NotFound},
		{name: "internal", err: errors.New("database unavailable"), code: codes.Internal},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := status.Code(mapError(testCase.err)); got != testCase.code {
				t.Fatalf("code=%s want=%s", got, testCase.code)
			}
		})
	}
}
