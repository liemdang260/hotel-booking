package http

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"strconv"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

type SearchHotelsUsecase interface {
	Execute(context.Context, domain.SearchInput) (domain.SearchResult, error)
}

type SearchHandler struct {
	search SearchHotelsUsecase
	maxBodyBytes int64
}

func NewSearchHandler(search SearchHotelsUsecase) *SearchHandler {
	return &SearchHandler{search: search, maxBodyBytes: 1024}
}

type publicError struct {
	Error struct {
		Code string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *SearchHandler) ServeHTTP(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
	if request.Method != stdhttp.MethodGet {
		writeError(writer, stdhttp.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	if request.ContentLength > h.maxBodyBytes {
		writeError(writer, stdhttp.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request is too large")
		return
	}
	query := request.URL.Query()
	checkIn, err := time.Parse(time.DateOnly, query.Get("check_in"))
	if err != nil {
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
		return
	}
	checkOut, err := time.Parse(time.DateOnly, query.Get("check_out"))
	if err != nil {
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
		return
	}
	guests, err := boundedInt32(query.Get("guests"))
	if err != nil {
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
		return
	}
	rooms, err := boundedInt32(query.Get("rooms"))
	if err != nil {
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
		return
	}
	pageSize := int32(0)
	if value := query.Get("page_size"); value != "" {
		pageSize, err = boundedInt32(value)
		if err != nil {
			writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
			return
		}
	}
	result, err := h.search.Execute(request.Context(), domain.SearchInput{
		City: query.Get("city"),
		CheckIn: checkIn.UTC(),
		CheckOut: checkOut.UTC(),
		GuestCount: guests,
		RoomQuantity: rooms,
		PageSize: pageSize,
		PageToken: query.Get("page_token"),
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidSearch):
			writeError(writer, stdhttp.StatusBadRequest, "INVALID_SEARCH", "invalid search request")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(writer, stdhttp.StatusGatewayTimeout, "SEARCH_TIMEOUT", "search timed out")
		default:
			writeError(writer, stdhttp.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "search is temporarily unavailable")
		}
		return
	}
	writeJSON(writer, stdhttp.StatusOK, result)
}

func boundedInt32(value string) (int32, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 {
		return 0, domain.ErrInvalidSearch
	}
	return int32(parsed), nil
}

func writeError(writer stdhttp.ResponseWriter, statusCode int, code, message string) {
	response := publicError{}
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(writer, statusCode, response)
}

func writeJSON(writer stdhttp.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}
