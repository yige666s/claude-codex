package httpjson

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"known":"ok","extra":true}`))
	var body struct {
		Known string `json:"known"`
	}

	err := Decode(req, &body)
	if err == nil {
		t.Fatal("expected decode error")
	}
	var decodeErr *Error
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if decodeErr.Status != http.StatusBadRequest || decodeErr.Code != "unknown_field" {
		t.Fatalf("decode error = status %d code %q", decodeErr.Status, decodeErr.Code)
	}
}

func TestDecodeRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"abcdef"}`))
	var body struct {
		Value string `json:"value"`
	}

	err := DecodeWithOptions(req, &body, DecodeOptions{
		MaxBytes:              8,
		DisallowUnknownFields: true,
	})
	if err == nil {
		t.Fatal("expected decode error")
	}
	var decodeErr *Error
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if decodeErr.Status != http.StatusRequestEntityTooLarge || decodeErr.Code != "payload_too_large" {
		t.Fatalf("decode error = status %d code %q", decodeErr.Status, decodeErr.Code)
	}
}

func TestDecodeOptionalAllowsEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	var body struct {
		Value string `json:"value" validate:"notblank"`
	}

	if err := DecodeOptional(req, &body); err != nil {
		t.Fatalf("DecodeOptional() error = %v", err)
	}
}

func TestDecodeValidatesStructTags(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"not-email","name":"   "}`))
	var body struct {
		Email string `json:"email" validate:"required,email"`
		Name  string `json:"name" validate:"notblank"`
	}

	err := Decode(req, &body)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var decodeErr *Error
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if decodeErr.Status != http.StatusBadRequest || decodeErr.Code != "validation_failed" {
		t.Fatalf("validation error = status %d code %q", decodeErr.Status, decodeErr.Code)
	}
	if !strings.Contains(decodeErr.Message, "email must be a valid email") {
		t.Fatalf("validation message missing email field: %q", decodeErr.Message)
	}
	if !strings.Contains(decodeErr.Message, "name is required") {
		t.Fatalf("validation message missing name field: %q", decodeErr.Message)
	}
}

func TestDecodeRunsRequestValidator(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"primary":"","fallback":""}`))
	var body fallbackRequest

	err := Decode(req, &body)
	if err == nil {
		t.Fatal("expected validation error")
	}
	var decodeErr *Error
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if decodeErr.Code != "validation_failed" || decodeErr.Message != "primary or fallback is required" {
		t.Fatalf("validation error = code %q message %q", decodeErr.Code, decodeErr.Message)
	}
}

type fallbackRequest struct {
	Primary  string `json:"primary"`
	Fallback string `json:"fallback"`
}

func (r fallbackRequest) ValidateRequest() error {
	if strings.TrimSpace(r.Primary) == "" && strings.TrimSpace(r.Fallback) == "" {
		return ValidationFailed("primary or fallback is required")
	}
	return nil
}

func TestDecodeRejectsMultipleJSONValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"a"} {"value":"b"}`))
	var body struct {
		Value string `json:"value"`
	}

	err := Decode(req, &body)
	if err == nil {
		t.Fatal("expected decode error")
	}
	var decodeErr *Error
	if !errors.As(err, &decodeErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if decodeErr.Status != http.StatusBadRequest || decodeErr.Code != "invalid_json" {
		t.Fatalf("decode error = status %d code %q", decodeErr.Status, decodeErr.Code)
	}
}
