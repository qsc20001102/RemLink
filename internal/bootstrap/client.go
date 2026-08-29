package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	appconfig "remlink/internal/config"
)

const maxResponseBody = 1 << 20

// Client calls the public Bootstrap API before Overlay connectivity exists.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if err := appconfig.ValidateServerURL(baseURL); err != nil {
		return nil, fmt.Errorf("invalid Bootstrap base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, http: httpClient}, nil
}

func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	var response ServerInfo
	if err := c.do(ctx, http.MethodGet, "/api/v1/server/info", nil, &response); err != nil {
		return ServerInfo{}, err
	}
	return response, nil
}

func (c *Client) Register(ctx context.Context, request RegisterRequest) (RegisterResponse, error) {
	var response RegisterResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/bootstrap/register", request, &response); err != nil {
		return RegisterResponse{}, err
	}
	return response, nil
}

func (c *Client) Config(ctx context.Context, request ConfigRequest) (ConfigResponse, error) {
	var response ConfigResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/bootstrap/config", request, &response); err != nil {
		return ConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Bootstrap request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create Bootstrap request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call Bootstrap API: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBody+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read Bootstrap response: %w", err)
	}
	if len(raw) > maxResponseBody {
		return errors.New("Bootstrap response exceeds 1 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope apiErrorEnvelope
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Code != "" {
			return &ClientError{Status: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		return &ClientError{Status: response.StatusCode, Code: "HTTP_ERROR", Message: strings.TrimSpace(string(raw))}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode Bootstrap response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("Bootstrap response must contain one JSON value")
	}
	return nil
}

// ClientError preserves the API status and machine-readable code.
type ClientError struct {
	Status  int
	Code    string
	Message string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("Bootstrap API %s (HTTP %d): %s", e.Code, e.Status, e.Message)
}
