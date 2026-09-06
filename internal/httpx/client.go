// SPDX-License-Identifier: Apache-2.0

// Package httpx builds the tuned HTTP transport shared by the Telegram and
// Deepgram clients. Go's default MaxIdleConnsPerHost is 2, which serializes
// concurrent requests against a single host: once updates are handled by a pool
// of workers, that default would quietly undo the parallelism.
//
// Per-request deadlines belong at the call site via context, not here, so
// callers stay in control of cancellation.
package httpx

import (
	"net"
	"net/http"
	"time"
)

// New returns a client with no global timeout. Every caller must apply its own
// context deadline.
func New() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}}
}
