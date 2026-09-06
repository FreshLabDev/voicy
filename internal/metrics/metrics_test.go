// SPDX-License-Identifier: Apache-2.0
package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExpositionFormat(t *testing.T) {
	TranscriptsSent.Inc()
	JobFailures.Inc("deepgram")
	JobFailures.Inc("deepgram")
	JobFailures.Inc("download")

	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type = %q", ct)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"# TYPE voicy_transcripts_sent_total counter",
		"voicy_transcripts_sent_total 1",
		`voicy_job_failures_total{stage="deepgram"} 2`,
		`voicy_job_failures_total{stage="download"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	// Every metric needs its HELP and TYPE lines or a scraper rejects the page.
	for _, e := range registry {
		if !strings.Contains(body, "# HELP "+e.name+" ") || !strings.Contains(body, "# TYPE "+e.name+" counter") {
			t.Fatalf("metric %s is missing HELP or TYPE", e.name)
		}
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	vec := registerVec("voicy_escaping_probe_total", "Probe.", "reason")
	vec.Inc(`we"ird\value` + "\n")
	var sb strings.Builder
	Write(&sb)
	if !strings.Contains(sb.String(), `voicy_escaping_probe_total{reason="we\"ird\\value\n"} 1`) {
		t.Fatalf("label was not escaped:\n%s", sb.String())
	}
}

// Counter names carry the bot prefix and the _total suffix a scraper expects.
func TestNamingConvention(t *testing.T) {
	for _, e := range registry {
		if !strings.HasPrefix(e.name, "voicy_") || !strings.HasSuffix(e.name, "_total") {
			t.Fatalf("metric %q breaks the naming convention", e.name)
		}
		if e.help == "" || !strings.HasSuffix(e.help, ".") {
			t.Fatalf("metric %q needs a sentence of help text", e.name)
		}
	}
}
