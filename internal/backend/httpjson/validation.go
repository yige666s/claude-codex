package httpjson

import (
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/go-playground/validator/v10"
)

var requestValidator = newRequestValidator()

type RequestValidator interface {
	ValidateRequest() error
}

func Validate(v any) error {
	if v == nil {
		return nil
	}
	if err := requestValidator.Struct(v); err != nil {
		return normalizeValidationError(err)
	}
	if err := validateRequest(v); err != nil {
		return normalizeRequestValidationError(err)
	}
	return nil
}

func ValidationFailed(message string) error {
	return decodeError(http.StatusBadRequest, "validation_failed", strings.TrimSpace(message), nil)
}

func newRequestValidator() *validator.Validate {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	_ = validate.RegisterValidation("notblank", func(level validator.FieldLevel) bool {
		field := level.Field()
		if field.Kind() == reflect.String {
			return strings.TrimSpace(field.String()) != ""
		}
		return !field.IsZero()
	})
	return validate
}

func normalizeValidationError(err error) error {
	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return decodeError(http.StatusBadRequest, "validation_failed", "request validation failed", err)
	}
	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return decodeError(http.StatusBadRequest, "validation_failed", "request validation failed", err)
	}
	messages := make([]string, 0, len(validationErrors))
	for _, fieldErr := range validationErrors {
		messages = append(messages, validationMessage(fieldErr))
	}
	sort.Strings(messages)
	return decodeError(http.StatusBadRequest, "validation_failed", strings.Join(messages, "; "), err)
}

func validateRequest(v any) error {
	if validator, ok := v.(RequestValidator); ok {
		return validator.ValidateRequest()
	}
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil
	}
	if validator, ok := value.Elem().Interface().(RequestValidator); ok {
		return validator.ValidateRequest()
	}
	return nil
}

func normalizeRequestValidationError(err error) error {
	if err == nil {
		return nil
	}
	var httpErr *Error
	if errors.As(err, &httpErr) {
		return httpErr
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "request validation failed"
	}
	return decodeError(http.StatusBadRequest, "validation_failed", message, err)
}

func validationMessage(fieldErr validator.FieldError) string {
	field := validationFieldName(fieldErr)
	switch fieldErr.Tag() {
	case "required", "notblank":
		return field + " is required"
	case "email":
		return field + " must be a valid email"
	case "min":
		return field + " must be at least " + fieldErr.Param()
	case "max":
		return field + " must be at most " + fieldErr.Param()
	case "gt":
		return field + " must be greater than " + fieldErr.Param()
	case "gte":
		return field + " must be greater than or equal to " + fieldErr.Param()
	case "lt":
		return field + " must be less than " + fieldErr.Param()
	case "lte":
		return field + " must be less than or equal to " + fieldErr.Param()
	case "oneof":
		return field + " must be one of: " + fieldErr.Param()
	default:
		return field + " is invalid"
	}
}

func validationFieldName(fieldErr validator.FieldError) string {
	namespace := fieldErr.Namespace()
	if namespace != "" {
		if idx := strings.Index(namespace, "."); idx >= 0 && idx+1 < len(namespace) {
			return namespace[idx+1:]
		}
		return namespace
	}
	if field := fieldErr.Field(); field != "" {
		return field
	}
	return "request"
}
