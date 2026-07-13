// Package domain defines transport-neutral error codes for Anito operations.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

type Code string

const (
	CodeMissingService   Code = "missing_service"
	CodeInvalidConfig    Code = "invalid_config"
	CodeReadinessFailure Code = "readiness_failure"
	CodeConflict         Code = "conflict"
)

type Error struct {
	Code    Code              `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Errorf(code Code, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) WithDetail(key, value string) *Error {
	if e == nil || key == "" || value == "" {
		return e
	}
	if e.Details == nil {
		e.Details = map[string]string{}
	}
	e.Details[key] = value
	return e
}

func MissingServicef(format string, args ...any) *Error {
	return Errorf(CodeMissingService, format, args...)
}

func InvalidConfigf(format string, args ...any) *Error {
	return Errorf(CodeInvalidConfig, format, args...)
}

func ReadinessFailuref(format string, args ...any) *Error {
	return Errorf(CodeReadinessFailure, format, args...)
}

func Conflictf(format string, args ...any) *Error {
	return Errorf(CodeConflict, format, args...)
}

func CodeOf(err error) (Code, bool) {
	var de *Error
	if errors.As(err, &de) && de != nil {
		return de.Code, true
	}
	return "", false
}

type WireError struct {
	Code    Code              `json:"code"`
	Error   string            `json:"error"`
	Message string            `json:"message,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

func ToWire(err error) WireError {
	var de *Error
	if errors.As(err, &de) && de != nil {
		msg := Redact(de.Message)
		if msg == "" {
			msg = Redact(de.Error())
		}
		return WireError{Code: de.Code, Error: msg, Message: msg, Details: redactDetails(de.Details)}
	}
	msg := ""
	if err != nil {
		msg = Redact(err.Error())
	}
	return WireError{Error: msg, Message: msg}
}

func redactDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]string, len(details))
	for k, v := range details {
		out[k] = Redact(v)
	}
	return out
}

func FromWire(data []byte) (*Error, bool) {
	var wire WireError
	if err := json.Unmarshal(data, &wire); err != nil || wire.Code == "" {
		return nil, false
	}
	msg := wire.Message
	if msg == "" {
		msg = wire.Error
	}
	if msg == "" {
		msg = string(wire.Code)
	}
	return &Error{Code: wire.Code, Message: msg, Details: wire.Details}, true
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password)(\s*[:=]\s*)([^\s,;]+)`),
	regexp.MustCompile(`(?i)\b(bearer)(\s+)([^\s,;]+)`),
}

func Redact(s string) string {
	out := s
	for _, pattern := range secretPatterns {
		out = pattern.ReplaceAllString(out, `${1}${2}[redacted]`)
	}
	return out
}
