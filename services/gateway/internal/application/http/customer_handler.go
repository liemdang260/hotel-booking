package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liemdang260/hotel-booking/services/gateway/internal/domain"
)

const defaultPageSize int32 = 20

type Authenticator interface {
	Authenticate(context.Context, string) (domain.Principal, error)
}

type CustomerAPI interface {
	CreateQuote(context.Context, domain.Principal, domain.CreateQuoteInput) (domain.Quote, error)
	CreateBooking(context.Context, domain.CreateBookingInput) (domain.Booking, error)
	GetBooking(context.Context, domain.GetBookingInput) (domain.Booking, error)
	ListBookings(context.Context, domain.ListBookingsInput) (domain.BookingPage, error)
	CancelBooking(context.Context, domain.CancelBookingInput) (domain.CancellationResult, error)
}

type CustomerHandler struct {
	auth Authenticator
	api CustomerAPI
	maxBodyBytes int64
	maxHeaderBytes int
}

func NewCustomerHandler(auth Authenticator, api CustomerAPI, maxBodyBytes int64, maxHeaderBytes int) (*CustomerHandler, error) {
	if auth == nil || api == nil || maxBodyBytes < 1 || maxBodyBytes > 1<<20 || maxHeaderBytes < 1 || maxHeaderBytes > 64<<10 {
		return nil, domain.ErrInvalidRequest
	}
	return &CustomerHandler{auth: auth, api: api, maxBodyBytes: maxBodyBytes, maxHeaderBytes: maxHeaderBytes}, nil
}

func (h *CustomerHandler) ServeHTTP(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
	if headerSize(request.Header) > h.maxHeaderBytes {
		writeError(writer, stdhttp.StatusRequestHeaderFieldsTooLarge, "REQUEST_HEADERS_TOO_LARGE", "request headers are too large")
		return
	}
	sanitizeIdentityHeaders(request.Header)
	actor, err := h.auth.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil || !actor.Valid() {
		writeError(writer, stdhttp.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required")
		return
	}

	path := strings.TrimSuffix(request.URL.Path, "/")
	switch {
	case request.Method == stdhttp.MethodPost && path == "/api/v1/quotes":
		h.createQuote(writer, request, actor)
	case request.Method == stdhttp.MethodPost && path == "/api/v1/bookings":
		h.createBooking(writer, request, actor)
	case request.Method == stdhttp.MethodGet && path == "/api/v1/bookings":
		h.listBookings(writer, request, actor)
	case request.Method == stdhttp.MethodGet && strings.HasPrefix(path, "/api/v1/bookings/"):
		h.getBooking(writer, request, actor, strings.TrimPrefix(path, "/api/v1/bookings/"))
	case request.Method == stdhttp.MethodPost && strings.HasPrefix(path, "/api/v1/bookings/") && strings.HasSuffix(path, "/cancel"):
		bookingID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/bookings/"), "/cancel")
		h.cancelBooking(writer, request, actor, bookingID)
	default:
		writeError(writer, stdhttp.StatusNotFound, "NOT_FOUND", "route not found")
	}
}

type createQuoteRequest struct {
	HotelID string `json:"hotel_id"`
	RoomTypeID string `json:"room_type_id"`
	CheckIn string `json:"check_in"`
	CheckOut string `json:"check_out"`
	Guests int32 `json:"guests"`
	Rooms int32 `json:"rooms"`
}

type createBookingRequest struct {
	QuoteID string `json:"quote_id"`
	PaymentMethodID string `json:"payment_method_id"`
}

type cancelBookingRequest struct {
	Reason string `json:"reason"`
}

func (h *CustomerHandler) createQuote(writer stdhttp.ResponseWriter, request *stdhttp.Request, actor domain.Principal) {
	var payload createQuoteRequest
	if !h.decode(writer, request, &payload) {
		return
	}
	checkIn, err := time.Parse(time.DateOnly, payload.CheckIn)
	if err != nil {
		h.writeMappedError(writer, domain.ErrInvalidRequest)
		return
	}
	checkOut, err := time.Parse(time.DateOnly, payload.CheckOut)
	if err != nil {
		h.writeMappedError(writer, domain.ErrInvalidRequest)
		return
	}
	result, err := h.api.CreateQuote(request.Context(), actor, domain.CreateQuoteInput{
		HotelID: payload.HotelID, RoomTypeID: payload.RoomTypeID,
		CheckIn: checkIn.UTC(), CheckOut: checkOut.UTC(),
		GuestCount: payload.Guests, RoomQuantity: payload.Rooms,
	})
	if err != nil {
		h.writeMappedError(writer, err)
		return
	}
	writeJSON(writer, stdhttp.StatusCreated, map[string]any{
		"quote_id": result.ID, "currency": result.Currency,
		"subtotal_minor": result.SubtotalMinor, "tax_minor": result.TaxMinor,
		"service_fee_minor": result.ServiceFeeMinor, "discount_minor": result.DiscountMinor,
		"total_minor": result.TotalMinor, "expires_at": result.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func (h *CustomerHandler) createBooking(writer stdhttp.ResponseWriter, request *stdhttp.Request, actor domain.Principal) {
	var payload createBookingRequest
	if !h.decode(writer, request, &payload) {
		return
	}
	result, err := h.api.CreateBooking(request.Context(), domain.CreateBookingInput{
		Actor: actor, QuoteID: payload.QuoteID, PaymentMethodID: payload.PaymentMethodID,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		h.writeMappedError(writer, err)
		return
	}
	writeJSON(writer, stdhttp.StatusCreated, publicBooking(result))
}

func (h *CustomerHandler) getBooking(writer stdhttp.ResponseWriter, request *stdhttp.Request, actor domain.Principal, bookingID string) {
	if strings.Contains(bookingID, "/") {
		writeError(writer, stdhttp.StatusNotFound, "NOT_FOUND", "route not found")
		return
	}
	result, err := h.api.GetBooking(request.Context(), domain.GetBookingInput{Actor: actor, BookingID: bookingID})
	if err != nil {
		h.writeMappedError(writer, err)
		return
	}
	writeJSON(writer, stdhttp.StatusOK, publicBooking(result))
}

func (h *CustomerHandler) listBookings(writer stdhttp.ResponseWriter, request *stdhttp.Request, actor domain.Principal) {
	pageSize := defaultPageSize
	if value := request.URL.Query().Get("page_size"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			h.writeMappedError(writer, domain.ErrInvalidRequest)
			return
		}
		pageSize = int32(parsed)
	}
	result, err := h.api.ListBookings(request.Context(), domain.ListBookingsInput{
		Actor: actor, PageSize: pageSize, PageToken: request.URL.Query().Get("page_token"),
	})
	if err != nil {
		h.writeMappedError(writer, err)
		return
	}
	bookings := make([]map[string]any, 0, len(result.Bookings))
	for _, booking := range result.Bookings {
		bookings = append(bookings, publicBooking(booking))
	}
	writeJSON(writer, stdhttp.StatusOK, map[string]any{"bookings": bookings, "next_page_token": result.NextPageToken})
}

func (h *CustomerHandler) cancelBooking(writer stdhttp.ResponseWriter, request *stdhttp.Request, actor domain.Principal, bookingID string) {
	if bookingID == "" || strings.Contains(bookingID, "/") {
		writeError(writer, stdhttp.StatusNotFound, "NOT_FOUND", "route not found")
		return
	}
	var payload cancelBookingRequest
	if !h.decode(writer, request, &payload) {
		return
	}
	result, err := h.api.CancelBooking(request.Context(), domain.CancelBookingInput{
		Actor: actor, BookingID: bookingID, IdempotencyKey: request.Header.Get("Idempotency-Key"), Reason: payload.Reason,
	})
	if err != nil {
		h.writeMappedError(writer, err)
		return
	}
	writeJSON(writer, stdhttp.StatusOK, map[string]any{
		"booking": publicBooking(result.Booking),
		"cancellation_id": result.CancellationID,
		"cancellation_state": result.State,
	})
}

func (h *CustomerHandler) decode(writer stdhttp.ResponseWriter, request *stdhttp.Request, target any) bool {
	if request.ContentLength > h.maxBodyBytes {
		writeError(writer, stdhttp.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request is too large")
		return false
	}
	request.Body = stdhttp.MaxBytesReader(writer, request.Body, h.maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(writer, stdhttp.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "request is too large")
		} else {
			writeError(writer, stdhttp.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		return false
	}
	return true
}

func (h *CustomerHandler) writeMappedError(writer stdhttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		writeError(writer, stdhttp.StatusBadRequest, "INVALID_REQUEST", "invalid request")
	case errors.Is(err, domain.ErrUnauthenticated):
		writeError(writer, stdhttp.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required")
	case errors.Is(err, domain.ErrForbidden):
		writeError(writer, stdhttp.StatusForbidden, "FORBIDDEN", "access denied")
	case errors.Is(err, domain.ErrNotFound):
		writeError(writer, stdhttp.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(writer, stdhttp.StatusConflict, "CONFLICT", "request conflicts with existing state")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(writer, stdhttp.StatusGatewayTimeout, "DEADLINE_EXCEEDED", "request timed out")
	case errors.Is(err, context.Canceled):
		writeError(writer, 499, "REQUEST_CANCELED", "request canceled")
	default:
		writeError(writer, stdhttp.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service temporarily unavailable")
	}
}

func publicBooking(value domain.Booking) map[string]any {
	return map[string]any{
		"id": value.ID, "hotel_id": value.HotelID, "room_type_id": value.RoomTypeID,
		"check_in": value.CheckIn.UTC().Format(time.DateOnly), "check_out": value.CheckOut.UTC().Format(time.DateOnly),
		"guest_count": value.GuestCount, "room_quantity": value.RoomQuantity,
		"status": value.Status, "total_minor": value.TotalMinor, "currency": value.Currency,
		"created_at": value.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func sanitizeIdentityHeaders(header stdhttp.Header) {
	for _, name := range []string{"X-User-ID", "X-Roles", "X-Service-Name", "X-Internal-Principal", "X-Authenticated-Subject"} {
		header.Del(name)
	}
}

func headerSize(header stdhttp.Header) int {
	total := 0
	for name, values := range header {
		total += len(name)
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}
