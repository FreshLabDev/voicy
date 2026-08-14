// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	DefaultREST = "https://api.deepgram.com"
	DefaultWS   = "wss://api.deepgram.com"
	ListenPath  = "/v1/listen"
)

type DialFunc func(ctx context.Context, endpoint string, header http.Header) (Conn, error)

type Conn interface {
	Write(ctx context.Context, typ websocket.MessageType, data []byte) error
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Close(websocket.StatusCode, string) error
}

type wsConn struct{ c *websocket.Conn }

func (w wsConn) Write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	return w.c.Write(ctx, typ, data)
}
func (w wsConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	return w.c.Read(ctx)
}
func (w wsConn) Close(code websocket.StatusCode, reason string) error {
	return w.c.Close(code, reason)
}

type Client struct {
	apiKey  string
	rest    string
	ws      string
	http    *http.Client
	dial    DialFunc
	timeout time.Duration
}

func New(apiKey string) *Client {
	c := &Client{
		apiKey:  apiKey,
		rest:    DefaultREST,
		ws:      DefaultWS,
		http:    &http.Client{Timeout: 2 * time.Minute},
		timeout: 45 * time.Second,
	}
	c.dial = c.defaultDial
	return c
}

func (c *Client) SetRESTBase(base string) { c.rest = strings.TrimRight(base, "/") }
func (c *Client) SetWSBase(base string)   { c.ws = strings.TrimRight(base, "/") }
func (c *Client) SetDial(d DialFunc) {
	if d != nil {
		c.dial = d
	}
}
func (c *Client) SetHTTP(h *http.Client) {
	if h != nil {
		c.http = h
	}
}

func (c *Client) defaultDial(ctx context.Context, endpoint string, header http.Header) (Conn, error) {
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, redact(c.apiKey, err)
	}
	return wsConn{c: conn}, nil
}

func streamQuery() string {
	q := url.Values{}
	q.Set("model", "nova-3")
	q.Set("language", "multi")
	q.Set("smart_format", "true")
	q.Set("interim_results", "true")
	q.Set("mip_opt_out", "true")
	return q.Encode()
}

func restQuery() string {
	q := url.Values{}
	q.Set("model", "nova-3")
	q.Set("smart_format", "true")
	q.Set("detect_language", "true")
	q.Set("paragraphs", "true")
	q.Set("mip_opt_out", "true")
	return q.Encode()
}

func (c *Client) authHeader() http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Token "+c.apiKey)
	return h
}

// StreamURL is the live Listen endpoint this client dials.
func (c *Client) StreamURL() string {
	return c.ws + ListenPath + "?" + streamQuery()
}

// StreamFile sends already-downloaded audio over the Live WS and reports partials.
func (c *Client) StreamFile(ctx context.Context, audio []byte, contentType string, onPartial func(string)) (Result, error) {
	if len(audio) == 0 {
		return Result{}, fmt.Errorf("empty audio")
	}
	header := c.authHeader()
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	conn, err := c.dial(ctx, c.StreamURL(), header)
	if err != nil {
		return Result{}, fmt.Errorf("deepgram stream dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	errCh := make(chan error, 1)
	var finals []string
	var last Result
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, payload, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			part, err := ExtractStream(payload)
			if err != nil {
				continue
			}
			if part.RequestID != "" {
				last.RequestID = part.RequestID
			}
			if part.Language != "" {
				last.Language = part.Language
			}
			if part.Duration > last.Duration {
				last.Duration = part.Duration
			}
			if part.Confidence > last.Confidence {
				last.Confidence = part.Confidence
			}
			var display string
			finals, display = Accumulate(finals, part)
			last.Text = display
			last.WordCount = len(strings.Fields(display))
			if display != "" && onPartial != nil {
				onPartial(display)
			}
		}
	}()

	const chunk = 32 * 1024
	for off := 0; off < len(audio); off += chunk {
		end := off + chunk
		if end > len(audio) {
			end = len(audio)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, audio[off:end]); err != nil {
			return last, redact(c.apiKey, err)
		}
	}
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Finalize"}`))
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"CloseStream"}`))

	select {
	case <-done:
	case <-time.After(c.timeout):
	case <-ctx.Done():
		return last, ctx.Err()
	}
	select {
	case err := <-errCh:
		if err != nil && last.Text == "" && !isNormalClose(err) {
			return last, redact(c.apiKey, err)
		}
	default:
	}
	last.IsFinal = true
	return last, nil
}

func isNormalClose(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "status = 1000") ||
		strings.Contains(err.Error(), "StatusNormalClosure") ||
		strings.Contains(err.Error(), "closed"))
}

func (c *Client) ListenFile(ctx context.Context, audio []byte, contentType string) (Result, error) {
	if contentType == "" {
		contentType = "audio/ogg"
	}
	endpoint := c.rest + ListenPath + "?" + restQuery()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(audio))
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("deepgram listen HTTP %d", resp.StatusCode)
	}
	return ExtractPrerecorded(body)
}

func (c *Client) Transcribe(ctx context.Context, audio []byte, contentType string, onPartial func(string)) (Result, error) {
	return c.ListenFile(ctx, audio, contentType)
}

func redact(key string, err error) error {
	if err == nil || key == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "***"))
}
