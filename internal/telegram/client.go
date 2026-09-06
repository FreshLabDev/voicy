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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/voicy/internal/httpx"
	"github.com/FreshLabDev/voicy/internal/metrics"
)

type Client struct {
	token   string
	apiBase string
	http    *http.Client
	timeout time.Duration
	sleep   func(context.Context, time.Duration) error
}

func NewClient(token string) *Client {
	c := httpx.New()
	// Long polling and file downloads set their own deadlines; this is the
	// backstop for a request that somehow escapes both.
	c.Timeout = 5 * time.Minute
	return &Client{
		token:   token,
		apiBase: "https://api.telegram.org",
		http:    c,
		timeout: 30 * time.Second,
		sleep:   sleep,
	}
}

func (c *Client) SetAPIBase(base string) {
	if base != "" {
		c.apiBase = strings.TrimRight(base, "/")
	}
}

// noLinkPreview replaces the removed disable_web_page_preview parameter.
var noLinkPreview = map[string]any{"is_disabled": true}

// richHTML builds an InputRichMessage carrying HTML. Exactly one of html,
// markdown or blocks may be set, and entity detection stays off so phone
// numbers, hashtags and card-like digits inside speech are not linkified.
func richHTML(body string) map[string]any {
	return map[string]any{
		"html":                  body,
		"skip_entity_detection": true,
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
		"chat_id":              chatID,
		"text":                 text,
		"parse_mode":           "HTML",
		"link_preview_options": noLinkPreview,
	}, markup)
}

// SendEphemeralMessage posts a message only the receiver can see. Bot API 10.3
// replaced the flat receiver_user_id parameter with ephemeral_message_parameters;
// the reply target is what authorizes the send inside Telegram's 15-second window.
func (c *Client) SendEphemeralMessage(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string, markup *InlineKeyboardMarkup) (Message, error) {
	req := map[string]any{
		"chat_id":                      chatID,
		"ephemeral_message_parameters": map[string]any{"receiver_user_id": receiverUserID},
		"text":                         text,
		"parse_mode":                   "HTML",
		"link_preview_options":         noLinkPreview,
		"reply_parameters": map[string]any{
			"ephemeral_message_id": ephemeralMessageID,
		},
	}
	return c.sendMessage(ctx, req, markup)
}

// SendRichHTML sends one rich message. The payload is HTML, so it belongs in
// InputRichMessage.html: the markdown field would additionally parse GFM syntax
// and mangle transcripts that contain *, _, #, |, backticks or list-like lines.
func (c *Client) SendRichHTML(ctx context.Context, chatID, replyTo int64, threadID int, body string, markup *InlineKeyboardMarkup) (Message, error) {
	req := map[string]any{
		"chat_id":      chatID,
		"rich_message": richHTML(body),
	}
	if replyTo > 0 {
		req["reply_parameters"] = map[string]any{
			"message_id":                  replyTo,
			"allow_sending_without_reply": true,
		}
	}
	if threadID > 0 {
		req["message_thread_id"] = threadID
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	return c.sendRichMessage(ctx, req)
}

func (c *Client) sendRichMessage(ctx context.Context, req map[string]any) (Message, error) {
	var resp struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := c.post(ctx, "sendRichMessage", req, &resp); err != nil {
		return Message{}, err
	}
	if !resp.OK {
		return Message{}, fmt.Errorf("telegram sendRichMessage returned ok=false")
	}
	return resp.Result, nil
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

func (c *Client) EditEphemeralMessageText(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string, markup *InlineKeyboardMarkup) error {
	return c.editEphemeral(ctx, chatID, receiverUserID, ephemeralMessageID, map[string]any{
		"text":       text,
		"parse_mode": "HTML",
	}, markup)
}

// EditEphemeralRichHTML replaces an ephemeral placeholder with rich content.
// Bot API 10.3 added rich_message to editEphemeralMessageText, which is the only
// way to deliver more than 4096 characters privately: a fresh ephemeral message
// cannot be sent once transcription has pushed us past Telegram's 15-second
// reply window.
func (c *Client) EditEphemeralRichHTML(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, body string, markup *InlineKeyboardMarkup) error {
	return c.editEphemeral(ctx, chatID, receiverUserID, ephemeralMessageID, map[string]any{
		"rich_message": richHTML(body),
	}, markup)
}

func (c *Client) editEphemeral(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, content map[string]any, markup *InlineKeyboardMarkup) error {
	req := map[string]any{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
	}
	for k, v := range content {
		req[k] = v
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "editEphemeralMessageText", req, &resp); err != nil {
		if messageNotModified(err) {
			return nil
		}
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram editEphemeralMessageText returned ok=false")
	}
	return nil
}

func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, markup *InlineKeyboardMarkup) error {
	req := map[string]any{
		"chat_id":              chatID,
		"message_id":           messageID,
		"text":                 text,
		"parse_mode":           "HTML",
		"link_preview_options": noLinkPreview,
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	err := c.post(ctx, "editMessageText", req, &resp)
	if messageNotModified(err) {
		return nil
	}
	return err
}

// EditMessageRichHTML replaces an ordinary message with rich content. It turns
// the "Transcribing…" placeholder in a direct chat into the transcript itself,
// so the user watches one message instead of waiting on an empty screen.
func (c *Client) EditMessageRichHTML(ctx context.Context, chatID, messageID int64, body string, markup *InlineKeyboardMarkup) error {
	req := map[string]any{
		"chat_id":      chatID,
		"message_id":   messageID,
		"rich_message": richHTML(body),
	}
	if markup != nil {
		req["reply_markup"] = markup
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	err := c.post(ctx, "editMessageText", req, &resp)
	if messageNotModified(err) {
		return nil
	}
	return err
}

func messageNotModified(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(apiErr.Description), "message is not modified")
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram deleteMessage returned ok=false")
	}
	return nil
}

func (c *Client) DeleteEphemeralMessage(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "deleteEphemeralMessage", map[string]any{
		"chat_id":              chatID,
		"receiver_user_id":     receiverUserID,
		"ephemeral_message_id": ephemeralMessageID,
	}, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram deleteEphemeralMessage returned ok=false")
	}
	return nil
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

func (c *Client) SendChatAction(ctx context.Context, chatID int64, threadID int, action string) error {
	req := map[string]any{"chat_id": chatID, "action": action}
	if threadID > 0 {
		req["message_thread_id"] = threadID
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "sendChatAction", req, &resp)
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

// DownloadFile fetches the media behind a getFile result.
//
// A Bot API server started with TELEGRAM_LOCAL answers with an absolute path on
// its own filesystem rather than a relative one. If that directory happens to be
// mounted here the bytes are read straight from disk and the file is removed,
// because a local server never reclaims them. It usually is not mounted: the
// server's data directory holds one subdirectory per bot named after that bot's
// token, so mounting it would hand Voicy every other bot's credentials. The
// normal path is therefore to make the path relative again and fetch it over
// the local network, which still lifts the cloud API's 20 MB ceiling.
func (c *Client) DownloadFile(ctx context.Context, filePath string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("download limit must be positive")
	}
	if strings.HasPrefix(filePath, "/") {
		body, err := c.readLocalFile(filePath, maxBytes)
		if err == nil {
			return body, nil
		}
		if errors.Is(err, errFileTooLarge) {
			return nil, err
		}
		filePath = relativeLocalPath(filePath, c.token)
	}
	downloadCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	rawURL := strings.TrimRight(c.apiBase, "/") + "/file/bot" + c.token + "/" + strings.TrimPrefix(filePath, "/")
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, rawURL, nil)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, c.redactError(err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("telegram file exceeds %d bytes", maxBytes)
	}
	return body, nil
}

// errFileTooLarge marks a limit that a second attempt cannot get past, so the
// HTTP fallback is not tried for a file we already know is oversize.
var errFileTooLarge = errors.New("telegram file exceeds the download limit")

// readLocalFile reads a file produced by a local Bot API server whose data
// directory is mounted here. The path comes from getFile, never from user input.
func (c *Client) readLocalFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("local bot api file unavailable: %w", err)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", errFileTooLarge, info.Size())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local bot api file: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", errFileTooLarge, len(body))
	}
	// A local server keeps every file it ever produced, and the directory is
	// shared with the other bots.
	_ = os.Remove(path)
	return body, nil
}

// relativeLocalPath turns "/var/lib/telegram-bot-api/<token>/voice/file_1.oga"
// back into "voice/file_1.oga" so it can be fetched from the same server over
// HTTP. An unrecognized shape is returned unchanged and fails loudly as a 404
// rather than silently downloading something else.
func relativeLocalPath(path, token string) string {
	if token == "" {
		return path
	}
	if _, rest, ok := strings.Cut(path, "/"+token+"/"); ok {
		return rest
	}
	return path
}

// LogOut releases the token from the current Bot API server so it can be used
// on another one. Telegram refuses to log back in to the cloud server for ten
// minutes afterwards, so this is only ever called deliberately.
func (c *Client) LogOut(ctx context.Context) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "logOut", map[string]any{}, &resp)
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
		if apiErr.StatusCode == http.StatusTooManyRequests {
			metrics.TelegramLimited.Inc(method)
		} else {
			metrics.TelegramErrors.Inc(method)
		}
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
	cause := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		cause = urlErr.Err
	}
	msg := cause.Error()
	if c.token != "" {
		msg = strings.ReplaceAll(msg, c.token, "***")
	}
	return &transportError{msg: "telegram transport failed: " + msg, cause: cause}
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
