package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type searchStub struct {
	result domain.SearchResult
	err    error
	input  domain.SearchInput
}

func (s *searchStub) Execute(_ context.Context, input domain.SearchInput) (domain.SearchResult, error) {
	s.input = input
	return s.result, s.err
}

func TestSearchHandlerMapsBoundedQueryToStableAdvisoryResponse(t *testing.T) {
	usecase := &searchStub{result: domain.SearchResult{Hotels: []domain.SearchHotel{}, Advisory: true}}
	handler := NewSearchHandler(usecase)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hotels/search?city=Tokyo&check_in=2026-09-01&check_out=2026-09-04&guests=2&rooms=1&page_size=20", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"advisory\":true") {
		t.Fatalf("code=%d body=%s", response.Code, response.Body.String())
	}
	if usecase.input.City != "Tokyo" || usecase.input.GuestCount != 2 || usecase.input.PageSize != 20 {
		t.Fatalf("input=%+v", usecase.input)
	}
}

func TestSearchHandlerDoesNotExposeDependencyDetails(t *testing.T) {
	usecase := &searchStub{err: errors.New("dial tcp internal-pricing:5432: refused")}
	handler := NewSearchHandler(usecase)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/hotels/search?city=Tokyo&check_in=2026-09-01&check_out=2026-09-04&guests=2&rooms=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusServiceUnavailable || strings.Contains(body, "internal-pricing") || !strings.Contains(body, "SEARCH_UNAVAILABLE") {
		t.Fatalf("code=%d body=%s", response.Code, body)
	}
}
