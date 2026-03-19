package sender

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/payload"
)

type Client struct {
	httpClient *http.Client
}

type preparedRequest struct {
	body            []byte
	contentEncoding string
}

type DeliveryError struct {
	Temporary  bool
	StatusCode int
	Err        error
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("collector request failed with status %d: %v", e.StatusCode, e.Err)
	}
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New() *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Client{
		httpClient: &http.Client{
			Transport: transport,
		},
	}
}

func (c *Client) Send(ctx context.Context, cfg *config.Runtime, batch payload.Envelope) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return &DeliveryError{
			Temporary: false,
			Err:       errors.New("collector token is empty"),
		}
	}

	raw, err := json.Marshal(batch)
	if err != nil {
		return &DeliveryError{Temporary: false, Err: err}
	}

	activeRequest := preparedRequest{body: raw}
	plainRequest := activeRequest
	if cfg.Transport.Compression {
		activeRequest, err = prepareCompressedRequest(raw)
		if err != nil {
			return &DeliveryError{Temporary: false, Err: err}
		}
	}

	backoff := cfg.Transport.InitialBackoff
	for attempt := 0; attempt <= cfg.Transport.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, jitter(backoff)); err != nil {
				return &DeliveryError{Temporary: true, Err: err}
			}
			backoff *= 2
			if backoff > cfg.Transport.MaxBackoff {
				backoff = cfg.Transport.MaxBackoff
			}
		}

		err := c.sendPrepared(ctx, cfg, activeRequest)
		if err == nil {
			return nil
		}

		if shouldRetryWithoutCompression(activeRequest, err) {
			activeRequest = plainRequest
			err = c.sendPrepared(ctx, cfg, activeRequest)
			if err == nil {
				return nil
			}
		}

		if !IsTemporary(err) || attempt == cfg.Transport.MaxRetries {
			return err
		}
	}

	return &DeliveryError{Temporary: true, Err: errors.New("delivery attempts exhausted")}
}

func (c *Client) sendPrepared(ctx context.Context, cfg *config.Runtime, reqBody preparedRequest) error {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Transport.RequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.CollectorURL, bytes.NewReader(reqBody.body))
	if err != nil {
		return &DeliveryError{Temporary: false, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("User-Agent", cfg.Transport.UserAgent)
	req.Header.Set("X-HubPulse-Agent-ID", cfg.AgentID)
	req.Header.Set("X-HubPulse-Config-Revision", cfg.ConfigRevision)
	if reqBody.contentEncoding != "" {
		req.Header.Set("Content-Encoding", reqBody.contentEncoding)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &DeliveryError{Temporary: true, Err: err}
	}
	defer resp.Body.Close()

	responsePreview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	message := fmt.Sprintf("collector returned %s", resp.Status)
	if detail := compactResponsePreview(responsePreview); detail != "" {
		message = fmt.Sprintf("%s: %s", message, detail)
	}

	return &DeliveryError{
		Temporary:  !isPermanentStatus(resp.StatusCode),
		StatusCode: resp.StatusCode,
		Err:        errors.New(message),
	}
}

func prepareCompressedRequest(raw []byte) (preparedRequest, error) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(raw); err != nil {
		return preparedRequest{}, err
	}
	if err := gzipWriter.Close(); err != nil {
		return preparedRequest{}, err
	}
	return preparedRequest{
		body:            compressed.Bytes(),
		contentEncoding: "gzip",
	}, nil
}

func shouldRetryWithoutCompression(req preparedRequest, err error) bool {
	if req.contentEncoding != "gzip" {
		return false
	}
	var deliveryErr *DeliveryError
	if !errors.As(err, &deliveryErr) {
		return false
	}
	switch deliveryErr.StatusCode {
	case http.StatusBadRequest, http.StatusUnsupportedMediaType:
		return true
	default:
		return false
	}
}

func compactResponsePreview(body []byte) string {
	preview := strings.TrimSpace(string(body))
	if preview == "" {
		return ""
	}
	preview = strings.Join(strings.Fields(preview), " ")
	if len(preview) > 240 {
		return preview[:240] + "..."
	}
	return preview
}

func isPermanentStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	if statusCode >= 500 {
		return false
	}
	return statusCode >= 400
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func jitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	maxJitter := delay / 5
	if maxJitter <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(int64(maxJitter)))
}

func IsTemporary(err error) bool {
	if err == nil {
		return false
	}
	var deliveryErr *DeliveryError
	if errors.As(err, &deliveryErr) {
		return deliveryErr.Temporary
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	var syscallErr syscall.Errno
	return errors.As(err, &syscallErr)
}
