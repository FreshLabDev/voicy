// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	token   string
	apiBase string
	http    *http.Client
	timeout time.Duration
	sleep   func(context.Context, time.Duration) error
}

func NewClient(token string) *Client {
	return &Client{
		token:   token,
		apiBase: "https://api.telegram.org",
		http:    &http.Client{},
		timeout: 30 * time.Second,
		sleep:   sleep,
	}
}

func (c *Client) SetAPIBase(base string) {
	if base != "" {
		c.apiBase = strings.TrimRight(base, "/")
	}
}

func (c *Client) DeleteWebhook(ctx context.Context) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, &resp)
}

func (c *Client) GetMe(ctx context.Context) (Me, error) {
	var resp struct {
		OK     bool `json:"ok"`
		Result Me   `json:"result"`
	}
	if err := c.get(ctx, "getMe", url.Values{}, &resp); err != nil {
		return Me{}, err
	}
	if !resp.OK {
		return Me{}, fmt.Errorf("telegram getMe returned ok=false")
	}
	return resp.Result, nil
}

func (c *Client) SetMyCommandsForScope(ctx context.Context, commands []BotCommand, scope *BotCommandScope) error {
	req := map[string]any{"commands": commands}
	if scope != nil {
		req["scope"] = scope
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "setMyCommands", req, &resp)
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	values := url.Values{}
	values.Set("timeout", strconv.Itoa(timeoutSeconds))
	values.Set("allowed_updates", `["message","callback_query","my_chat_member"]`)
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}
	var resp struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	reqTimeout := time.Duration(timeoutSeconds)*time.Second + 15*time.Second
	if err := c.getWithTimeout(ctx, "getUpdates", values, &resp, reqTimeout); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram getUpdates returned ok=false")
	}
	return resp.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, markup *InlineKeyboardMarkup) (Message, error) {
	return c.sendMessage(ctx, map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}, markup)
}

func (c *Client) SendPrivateMessage(ctx context.Context, chatID, receiverUserID int64, text string, threadID int) (Message, error) {
	req := map[string]any{
		"chat_id":                  chatID,
		"receiver_user_id":         receiverUserID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if threadID > 0 {
		req["message_thread_id"] = threadID
	}
	return c.sendMessage(ctx, req, nil)
}

func (c *Client) SendEphemeralMessage(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string, markup *InlineKeyboardMarkup) (Message, error) {
	req := map[string]any{
		"chat_id":                  chatID,
		"receiver_user_id":         receiverUserID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"reply_parameters": map[string]any{
			"ephemeral_message_id": ephemeralMessageID,
		},
	}
	return c.sendMessage(ctx, req, markup)
}

func (c *Client) SendReply(ctx context.Context, chatID, replyTo int64, threadID int, text string) (Message, error) {
	req := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if replyTo > 0 {
		req["reply_parameters"] = map[string]any{"message_id": replyTo}
	}
	if threadID > 0 {
		req["message_thread_id"] = threadID
	}
	return c.sendMessage(ctx, req, nil)
}

func (c *Client) sendMessage(ctx context.Context, req map[string]any, markup *InlineKeyboardMarkup) (Message, error) {
	if markup != nil {
		req["reply_markup"] = markup
	}
	var resp struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := c.post(ctx, "sendMessage", req, &resp); err != nil {
		return Message{}, err
	}
	if !resp.OK {
		return Message{}, fmt.Errorf("telegram sendMessage returned ok=false")
	}
	return resp.Result, nil
}

// SendMessageDraft streams a 30s preview. Groups often ignore it; callers must still send a final message.
func (c *Client) SendMessageDraft(ctx context.Context, chatID int64, draftID int, text string) error {
	req := map[string]any{
		"chat_id":  chatID,
		"draft_id": draftID,
		"text":     text,
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "sendMessageDraft", req, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram sendMessageDraft returned ok=false")
	}
	return nil
}

func (c *Client) EditEphemeralMessageText(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string) error {
	req := map[string]any{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
		"text":                 text,
		"parse_mode":           "HTML",
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "editEphemeralMessageText", req, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram editEphemeralMessageText returned ok=false")
	}
	return nil
}

func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	req := map[string]any{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "editMessageText", req, &resp)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	req := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		req["text"] = text
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "answerCallbackQuery", req, &resp)
}

func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": action}, &resp)
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	var resp struct {
		OK     bool `json:"ok"`
		Result File `json:"result"`
	}
	if err := c.get(ctx, "getFile", url.Values{"file_id": {fileID}}, &resp); err != nil {
		return File{}, err
	}
	if !resp.OK || resp.Result.FilePath == "" {
		return File{}, fmt.Errorf("telegram getFile returned empty path")
	}
	return resp.Result, nil
}

func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	rawURL := strings.TrimRight(c.apiBase, "/") + "/file/bot" + c.token + "/" + strings.TrimPrefix(filePath, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, c.redactError(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.redactError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError("downloadFile", resp.StatusCode, resp.Body)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 21<<20))
}

func (c *Client) get(ctx context.Context, method string, values url.Values, out any) error {
	return c.getWithTimeout(ctx, method, values, out, c.timeout)
}

func (c *Client) getWithTimeout(ctx context.Context, method string, values url.Values, out any, timeout time.Duration) error {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		retryAfter, err := c.attempt(ctx, timeout, func(attemptCtx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(attemptCtx, http.MethodGet, c.endpoint(method)+"?"+values.Encode(), nil)
		}, method, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == maxAttempts || !retryableError(err) {
			return err
		}
		delay := retryAfter
		if delay <= 0 {
			delay = retryDelay(attempt)
		}
		if err := c.sleep(ctx, delay); err != nil {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) post(ctx context.Context, method string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		retryAfter, err := c.attempt(ctx, c.timeout, func(attemptCtx context.Context) (*http.Request, error) {
			req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.endpoint(method), bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			return req, nil
		}, method, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if retryAfter <= 0 || attempt == 1 {
			return err
		}
		if err := c.sleep(ctx, retryAfter); err != nil {
			return err
		}
	}
	return lastErr
}

func (c *Client) attempt(ctx context.Context, timeout time.Duration, build func(context.Context) (*http.Request, error), method string, out any) (time.Duration, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := build(attemptCtx)
	if err != nil {
		return 0, c.redactError(err)
	}
	return c.do(method, req, out)
}

func (c *Client) do(method string, req *http.Request, out any) (time.Duration, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, c.redactError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := parseAPIError(method, resp.StatusCode, resp.Body)
		return apiErr.RetryAfter, apiErr
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return 0, nil
	}
	return 0, json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) redactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if c.token != "" {
		msg = strings.ReplaceAll(msg, c.token, "***")
	}
	cause := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		cause = urlErr.Err
	}
	return &transportError{msg: msg, cause: cause}
}

type transportError struct {
	msg   string
	cause error
}

func (e *transportError) Error() string { return e.msg }
func (e *transportError) Unwrap() error { return e.cause }

func retryableError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	}
	return !errors.Is(err, context.Canceled)
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 3 {
		attempt = 3
	}
	return jitterDuration(500 * time.Millisecond << (attempt - 1))
}

func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	half := int64(d / 2)
	return d - time.Duration(half) + time.Duration(rand.Int63n(2*half+1))
}

func (c *Client) endpoint(method string) string {
	return strings.TrimRight(c.apiBase, "/") + "/bot" + c.token + "/" + method
}

func parseAPIError(method string, statusCode int, body io.Reader) *APIError {
	raw, _ := io.ReadAll(io.LimitReader(body, 4096))
	var payload struct {
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter      int   `json:"retry_after"`
			MigrateToChatID int64 `json:"migrate_to_chat_id"`
		} `json:"parameters"`
	}
	description := strings.TrimSpace(string(raw))
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Description != "" {
		description = payload.Description
	}
	apiErr := &APIError{
		Method:          method,
		StatusCode:      statusCode,
		ErrorCode:       payload.ErrorCode,
		Description:     description,
		MigrateToChatID: payload.Parameters.MigrateToChatID,
	}
	if payload.Parameters.RetryAfter > 0 {
		apiErr.RetryAfter = time.Duration(payload.Parameters.RetryAfter) * time.Second
	}
	return apiErr
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
