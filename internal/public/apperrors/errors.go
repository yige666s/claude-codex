package apperrors

import (
	"errors"
	"fmt"
	"strings"
)

type Code string

const (
	CodeConfig     Code = "config"
	CodeAuth       Code = "auth"
	CodePermission Code = "permission"
	CodeDependency Code = "dependency"
	CodeInternal   Code = "internal"
)

type Error struct {
	Code    Code
	Message string
	Hint    string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(e.Message)
	if strings.TrimSpace(e.Hint) != "" {
		builder.WriteString("\nHint: ")
		builder.WriteString(e.Hint)
	}

	return builder.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Wrap(code Code, message, hint string, err error) error {
	if err == nil {
		return &Error{
			Code:    code,
			Message: message,
			Hint:    hint,
		}
	}

	var userErr *Error
	if errors.As(err, &userErr) {
		if userErr.Code == CodeInternal && code != CodeInternal {
			return &Error{
				Code:    code,
				Message: message,
				Hint:    hint,
				Err:     err,
			}
		}
		return err
	}

	return &Error{
		Code:    code,
		Message: message,
		Hint:    hint,
		Err:     err,
	}
}

func Config(message, hint string, err error) error {
	return Wrap(CodeConfig, message, hint, err)
}

func Auth(message, hint string, err error) error {
	return Wrap(CodeAuth, message, hint, err)
}

func Permission(message, hint string, err error) error {
	return Wrap(CodePermission, message, hint, err)
}

func Dependency(message, hint string, err error) error {
	return Wrap(CodeDependency, message, hint, err)
}

func Internal(message string, err error) error {
	return Wrap(CodeInternal, message, "", err)
}

func Detail(err error) string {
	if err == nil {
		return ""
	}

	var userErr *Error
	if errors.As(err, &userErr) {
		if userErr.Err != nil {
			return userErr.Err.Error()
		}
		return userErr.Message
	}

	return err.Error()
}

func FormatCLI(err error) string {
	if err == nil {
		return ""
	}

	var userErr *Error
	if errors.As(err, &userErr) {
		return userErr.Error()
	}

	return fmt.Sprintf("Internal error: %s", err)
}
