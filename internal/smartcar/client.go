package smartcar

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

const _maxErrorBodyBytes = 64 * 1024

type API interface {
	ListConnections(ctx context.Context, userID string) ([]Connection, error)
	GetVehicle(ctx context.Context, vehicleID string) (Vehicle, error)
	GetSignal(ctx context.Context, userID string, vehicleID string, signalCode string) (Signal, error)
}

var _ API = (*Client)(nil)

type Client struct {
	clientID       string
	clientSecret   string
	iamBaseURL     *url.URL
	vehicleBaseURL *url.URL
	httpClient     *http.Client
	unitSystem     string
	maxRetries     int
	userAgent      string
	logger         *zap.Logger
	observer       *obs.Observer

	tokenMu    sync.Mutex
	tokenCache accessToken
}

func NewClient(
	httpClient *http.Client,
	cfg config.SmartcarConfig,
	version string,
	logger *zap.Logger,
	observer *obs.Observer,
) (*Client, error) {
	iamBaseURL, err := url.Parse(cfg.IAMBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse iam base url: %w", err)
	}

	vehicleBaseURL, err := url.Parse(cfg.VehicleBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse vehicle base url: %w", err)
	}

	if httpClient == nil {
		httpClient = NewHTTPClient(cfg.Timeout, &tls.Config{MinVersion: tls.VersionTLS12})
	}

	userAgent := fmt.Sprintf("smartcar-4runner/%s", version)
	if version == "" {
		userAgent = "smartcar-4runner/dev"
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	if observer == nil {
		observer = &obs.Observer{}
	}

	return &Client{
		clientID:       cfg.ClientID,
		clientSecret:   cfg.ClientSecret,
		iamBaseURL:     iamBaseURL,
		vehicleBaseURL: vehicleBaseURL,
		httpClient:     httpClient,
		unitSystem:     cfg.UnitSystem,
		maxRetries:     cfg.MaxRetries,
		userAgent:      userAgent,
		logger:         logger,
		observer:       observer,
	}, nil
}

func (c *Client) ListConnections(ctx context.Context, userID string) ([]Connection, error) {
	ctx, span := c.observer.Tracer().Start(ctx, "smartcar.list_connections")
	defer span.End()

	operation := "list_connections"
	values := url.Values{}
	values.Set("filter[userId]", userID)
	values.Set("page[number]", "1")
	values.Set("page[size]", "100")
	nextPath := "/connections?" + values.Encode()

	connections := make([]Connection, 0, 4)
	for nextPath != "" {
		requestURL := c.resolveVehicleURL(nextPath)

		headers, body, err := c.doAuthorized(ctx, operation, func(token string) (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
			if err != nil {
				return nil, fmt.Errorf("new request: %w", err)
			}

			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", c.userAgent)

			return req, nil
		})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "list connections failed")
			return nil, err
		}

		_ = headers

		var response connectionsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			decodeErr := fmt.Errorf("decode connections response: %w", err)
			span.RecordError(decodeErr)
			span.SetStatus(codes.Error, "decode connections failed")
			return nil, decodeErr
		}

		pageConnections := make([]Connection, 0, len(response.Data))
		for _, item := range response.Data {
			pageConnections = append(pageConnections, decodeConnection(item))
		}

		connections = append(connections, pageConnections...)
		nextPath = response.Links.Next
	}

	span.SetAttributes(attribute.Int("smartcar.connections_count", len(connections)))

	return connections, nil
}

func (c *Client) GetVehicle(ctx context.Context, vehicleID string) (Vehicle, error) {
	ctx, span := c.observer.Tracer().Start(ctx, "smartcar.get_vehicle")
	defer span.End()

	span.SetAttributes(attribute.String("smartcar.vehicle_id", vehicleID))

	path := "/vehicles/" + url.PathEscape(vehicleID)
	headers, body, err := c.doAuthorized(ctx, "get_vehicle", func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolveVehicleURL(path), nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)

		return req, nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get vehicle failed")
		return Vehicle{}, err
	}

	_ = headers

	var response vehicleResponse
	if err := json.Unmarshal(body, &response); err != nil {
		decodeErr := fmt.Errorf("decode vehicle response: %w", err)
		span.RecordError(decodeErr)
		span.SetStatus(codes.Error, "decode vehicle failed")
		return Vehicle{}, decodeErr
	}

	return Vehicle{
		ID:             response.Data.ID,
		Make:           response.Data.Attributes.Make,
		Model:          response.Data.Attributes.Model,
		Year:           response.Data.Attributes.Year,
		PowertrainType: response.Data.Attributes.PowertrainType,
		Mode:           response.Data.Attributes.Mode,
	}, nil
}

func (c *Client) GetSignal(
	ctx context.Context,
	userID string,
	vehicleID string,
	signalCode string,
) (Signal, error) {
	ctx, span := c.observer.Tracer().Start(ctx, "smartcar.get_signal")
	defer span.End()

	span.SetAttributes(
		attribute.String("smartcar.vehicle_id", vehicleID),
		attribute.String("smartcar.signal_code", signalCode),
	)

	path := fmt.Sprintf(
		"/vehicles/%s/signals/%s",
		url.PathEscape(vehicleID),
		url.PathEscape(signalCode),
	)

	headers, body, err := c.doAuthorized(ctx, "get_signal", func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolveVehicleURL(path), nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("sc-user-id", userID)
		req.Header.Set("SC-Unit-System", c.unitSystem)

		return req, nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get signal failed")
		return Signal{}, err
	}

	payload := normalizeSignalPayload(body)
	signal := decodeSignal(signalCode, payload, headers)

	return signal, nil
}

func (c *Client) doAuthorized(
	ctx context.Context,
	operation string,
	buildRequest func(token string) (*http.Request, error),
) (http.Header, []byte, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, nil, err
	}

	return c.do(ctx, operation, func() (*http.Request, error) {
		return buildRequest(token)
	})
}

func (c *Client) do(
	ctx context.Context,
	operation string,
	buildRequest func() (*http.Request, error),
) (http.Header, []byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := buildRequest()
		if err != nil {
			return nil, nil, err
		}

		startedAt := time.Now()
		resp, err := c.httpClient.Do(req)
		duration := time.Since(startedAt)
		if err != nil {
			if shouldRetryError(err) && attempt < c.maxRetries {
				c.logger.Warn("smartcar transient request error",
					zap.String("operation", operation),
					zap.Int("attempt", attempt+1),
					zap.Error(err),
				)
				if sleepErr := sleepContext(ctx, retryDelay(nil, attempt)); sleepErr != nil {
					return nil, nil, sleepErr
				}
				continue
			}

			c.observer.RecordSmartcar(ctx, operation, "transport_error", duration, err)
			return nil, nil, err
		}

		bodyReader := io.Reader(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyReader = io.LimitReader(resp.Body, _maxErrorBodyBytes)
		}

		body, readErr := io.ReadAll(bodyReader)
		closeErr := resp.Body.Close()
		if readErr != nil {
			err = fmt.Errorf("read response body: %w", readErr)
		} else if closeErr != nil {
			err = fmt.Errorf("close response body: %w", closeErr)
		}
		if err != nil {
			c.observer.RecordSmartcar(ctx, operation, strconv.Itoa(resp.StatusCode), duration, err)
			return resp.Header, nil, err
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.observer.RecordSmartcar(ctx, operation, strconv.Itoa(resp.StatusCode), duration, nil)
			return resp.Header, body, nil
		}

		apiErr := &APIError{
			Operation: operation,
			Method:    req.Method,
			URL:       sanitizeURL(req.URL),
			Status:    resp.StatusCode,
			RequestID: resp.Header.Get("SC-Request-Id"),
			Body:      string(body),
		}

		if shouldRetryStatus(resp.StatusCode) && attempt < c.maxRetries {
			c.logger.Warn("smartcar retryable response",
				zap.String("operation", operation),
				zap.Int("status", resp.StatusCode),
				zap.Int("attempt", attempt+1),
				zap.String("request_id", apiErr.RequestID),
			)
			if sleepErr := sleepContext(ctx, retryDelay(resp.Header, attempt)); sleepErr != nil {
				return nil, nil, sleepErr
			}
			continue
		}

		c.observer.RecordSmartcar(ctx, operation, strconv.Itoa(resp.StatusCode), duration, apiErr)
		return resp.Header, body, apiErr
	}
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	now := time.Now()
	if c.tokenCache.valid(now) {
		return c.tokenCache.value, nil
	}

	token, err := c.requestAccessToken(ctx)
	if err != nil {
		c.observer.RecordToken(ctx, "error")
		return "", err
	}

	c.tokenCache = token
	c.observer.RecordToken(ctx, "ok")

	return token.value, nil
}

func (c *Client) requestAccessToken(ctx context.Context) (accessToken, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)

	headers, body, err := c.do(ctx, "token_request", func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.iamBaseURL.String()+"/oauth2/token",
			bytes.NewBufferString(form.Encode()),
		)
		if err != nil {
			return nil, fmt.Errorf("new token request: %w", err)
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)

		return req, nil
	})
	if err != nil {
		return accessToken{}, err
	}

	_ = headers

	var response tokenResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return accessToken{}, fmt.Errorf("decode token response: %w", err)
	}

	return accessToken{
		value:     response.AccessToken,
		expiresAt: time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}, nil
}

func decodeConnection(item connectionData) Connection {
	connection := Connection{
		ID:          item.ID,
		VehicleID:   item.Relationships.Vehicle.Data.ID,
		UserID:      item.Relationships.User.Data.ID,
		Permissions: append([]string(nil), item.Attributes.Permissions...),
		Vehicle: Vehicle{
			ID:             item.Relationships.Vehicle.Data.ID,
			Make:           item.Attributes.Vehicle.Make,
			Model:          item.Attributes.Vehicle.Model,
			Year:           item.Attributes.Vehicle.Year,
			PowertrainType: item.Attributes.Vehicle.PowertrainType,
			Mode:           item.Attributes.Vehicle.Mode,
		},
	}

	connection.CreatedAt = parseTime(item.Meta.CreatedAt)
	connection.UpdatedAt = parseTime(item.Meta.UpdatedAt)
	if item.Meta.LastUsedAt != nil {
		connection.LastUsedAt = parseTime(*item.Meta.LastUsedAt)
	}

	return connection
}

func decodeSignal(signalCode string, payload []byte, headers http.Header) Signal {
	signal := Signal{
		Code:         signalCode,
		Name:         signalNameFromCode(signalCode),
		Group:        signalGroupFromCode(signalCode),
		Status:       "ok",
		Body:         append([]byte(nil), payload...),
		RetrievedAt:  parseTime(headers.Get("SC-Fetched-At")),
		OEMUpdatedAt: parseTime(headers.Get("SC-Data-Age")),
		IngestedAt:   time.Now().UTC(),
		Unit:         headers.Get("SC-Unit-System"),
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		signal.Value = string(payload)
		return signal
	}

	if value, ok := decoded["value"]; ok {
		signal.Value = value
	} else {
		signal.Value = decoded
	}

	if unit, ok := decoded["unit"].(string); ok && unit != "" {
		signal.Unit = unit
	}

	if status, ok := decoded["status"].(string); ok && status != "" {
		signal.Status = status
	}

	return signal
}

func normalizeSignalPayload(body []byte) []byte {
	var envelope signalEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return envelope.Data
	}

	return body
}

func shouldRetryStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func shouldRetryError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

func retryDelay(headers http.Header, attempt int) time.Duration {
	if headers != nil {
		if value := headers.Get("Retry-After"); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil {
				return time.Duration(seconds) * time.Second
			}

			if when, err := http.ParseTime(value); err == nil {
				if delay := time.Until(when); delay > 0 {
					return delay
				}
			}
		}
	}

	base := 200 * time.Millisecond
	backoff := time.Duration(math.Pow(2, float64(attempt))) * base
	jitter := time.Duration(time.Now().UnixNano() % int64(100*time.Millisecond+1))

	return backoff + jitter
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

func sanitizeURL(value *url.URL) string {
	if value == nil {
		return ""
	}

	sanitized := &url.URL{
		Scheme: value.Scheme,
		Host:   value.Host,
		Path:   value.Path,
	}

	return sanitized.String()
}

func (c *Client) resolveVehicleURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	relative, err := url.Parse(path)
	if err != nil {
		return c.vehicleBaseURL.String()
	}

	return c.vehicleBaseURL.ResolveReference(relative).String()
}
