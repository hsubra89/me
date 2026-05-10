package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultHetznerEndpoint     = "https://api.hetzner.cloud/v1"
	hetznerValidationLimit     = 4 * time.Second
	hetznerReadValidationPath  = "/locations"
	hetznerWriteValidationPath = "/ssh_keys/0"
)

type hetznerValidator struct {
	endpoint string
	client   *http.Client
	timeout  time.Duration
}

type hetznerValidationError struct {
	reason  string
	timeout bool
}

func (e hetznerValidationError) Error() string {
	return e.reason
}

func (e hetznerValidationError) Timeout() bool {
	return e.timeout
}

func newHetznerValidator(endpoint string) hetznerValidator {
	if endpoint == "" {
		endpoint = defaultHetznerEndpoint
	}
	return hetznerValidator{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: hetznerValidationLimit},
		timeout:  hetznerValidationLimit,
	}
}

func (v hetznerValidator) validate(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return hetznerValidationError{reason: "token is empty"}
	}

	validateCtx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	if err := v.validateReadable(validateCtx, token); err != nil {
		return err
	}

	return v.validateWritable(validateCtx, token)
}

func (v hetznerValidator) validateReadable(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.endpoint+hetznerReadValidationPath, nil)
	if err != nil {
		return fmt.Errorf("build Hetzner validation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		if ctx.Err() != nil || os.IsTimeout(err) {
			return hetznerValidationError{reason: "did not validate within 4s", timeout: true}
		}
		return hetznerValidationError{reason: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return hetznerValidationError{reason: fmt.Sprintf("API rejected the token (%s)", resp.Status)}
	default:
		return hetznerValidationError{reason: fmt.Sprintf("Hetzner API returned %s", resp.Status)}
	}
}

func (v hetznerValidator) validateWritable(ctx context.Context, token string) error {
	// ID 0 is used as a non-existent key. A Read & Write token reaches the
	// not-found check, while a read-only token is rejected for using DELETE.
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, v.endpoint+hetznerWriteValidationPath, nil)
	if err != nil {
		return fmt.Errorf("build Hetzner write validation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		if ctx.Err() != nil || os.IsTimeout(err) {
			return hetznerValidationError{reason: "did not validate within 4s", timeout: true}
		}
		return hetznerValidationError{reason: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound:
		return nil
	case http.StatusUnauthorized:
		return hetznerValidationError{reason: fmt.Sprintf("API rejected the token (%s)", resp.Status)}
	case http.StatusForbidden:
		return hetznerValidationError{reason: "token is read-only; create a Read & Write Hetzner token"}
	default:
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		return hetznerValidationError{reason: fmt.Sprintf("Hetzner API returned %s", resp.Status)}
	}
}

func validationTimedOut(err error) bool {
	var validationErr hetznerValidationError
	if err != nil && errors.As(err, &validationErr) {
		return validationErr.Timeout()
	}

	return false
}
