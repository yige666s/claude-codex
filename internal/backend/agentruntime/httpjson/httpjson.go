package httpjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultMaxRequestBytes int64 = 10 << 20

type DecodeOptions struct {
	MaxBytes              int64
	AllowEmpty            bool
	DisallowUnknownFields bool
	SkipValidation        bool
}

type WriteOptions struct {
	Normalize func(w http.ResponseWriter, status int, value any) any
}

type Error struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "request body is invalid"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Decode(r *http.Request, v any) error {
	return DecodeWithOptions(r, v, defaultDecodeOptions(false))
}

func DecodeOptional(r *http.Request, v any) error {
	return DecodeWithOptions(r, v, defaultDecodeOptions(true))
}

func DecodeWithOptions(r *http.Request, v any, options DecodeOptions) error {
	if r == nil || r.Body == nil {
		if options.AllowEmpty {
			return nil
		}
		return decodeError(http.StatusBadRequest, "empty_body", "request body is required", nil)
	}
	defer r.Body.Close()

	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBytes
	}
	body := http.MaxBytesReader(nil, r.Body, maxBytes)
	decoder := json.NewDecoder(body)
	if options.DisallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(v); err != nil {
		if errors.Is(err, io.EOF) && options.AllowEmpty {
			return nil
		}
		return normalizeDecodeError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return decodeError(http.StatusBadRequest, "invalid_json", "request body must contain a single JSON value", nil)
	} else if !errors.Is(err, io.EOF) {
		return normalizeDecodeError(err)
	}
	if !options.SkipValidation {
		if err := Validate(v); err != nil {
			return err
		}
	}
	return nil
}

func Write(w http.ResponseWriter, status int, value any) {
	WriteWithOptions(w, status, value, WriteOptions{})
}

func WriteWithOptions(w http.ResponseWriter, status int, value any, options WriteOptions) {
	if options.Normalize != nil {
		value = options.Normalize(w, status, value)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Status(err error) int {
	var decodeErr *Error
	if errors.As(err, &decodeErr) && decodeErr.Status > 0 {
		return decodeErr.Status
	}
	return http.StatusBadRequest
}

func normalizeDecodeError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return decodeError(http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body must not exceed %d bytes", maxBytesErr.Limit), err)
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "request body is invalid JSON"
	}
	code := "invalid_json"
	if strings.Contains(message, "unknown field") {
		code = "unknown_field"
	}
	return decodeError(http.StatusBadRequest, code, message, err)
}

func decodeError(status int, code, message string, err error) error {
	return &Error{
		Status:  status,
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func defaultDecodeOptions(allowEmpty bool) DecodeOptions {
	return DecodeOptions{
		MaxBytes:              DefaultMaxRequestBytes,
		AllowEmpty:            allowEmpty,
		DisallowUnknownFields: true,
	}
}
