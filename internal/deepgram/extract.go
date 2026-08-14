// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"encoding/json"
	"strings"
)

// Result is the normalized transcript we persist and show.
type Result struct {
	Text        string
	Confidence  float64
	Language    string
	Duration    float64
	RequestID   string
	WordCount   int
	IsFinal     bool
	SpeechFinal bool
}

type prererecordedResponse struct {
	Metadata struct {
		RequestID string  `json:"request_id"`
		Duration  float64 `json:"duration"`
	} `json:"metadata"`
	Results struct {
		Channels []struct {
			DetectedLanguage string `json:"detected_language"`
			Alternatives     []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []any   `json:"words"`
				Paragraphs struct {
					Transcript string `json:"transcript"`
				} `json:"paragraphs"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

type streamResults struct {
	Type     string  `json:"type"`
	IsFinal  bool    `json:"is_final"`
	Duration float64 `json:"duration"`
	Metadata struct {
		RequestID string  `json:"request_id"`
		Duration  float64 `json:"duration"`
	} `json:"metadata"`
	Channel struct {
		DetectedLanguage string `json:"detected_language"`
		Alternatives     []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
			Words      []any   `json:"words"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// ExtractPrerecorded parses a Listen REST body.
func ExtractPrerecorded(raw []byte) (Result, error) {
	var body prererecordedResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return Result{}, err
	}
	out := Result{RequestID: body.Metadata.RequestID, Duration: body.Metadata.Duration, IsFinal: true}
	if len(body.Results.Channels) == 0 || len(body.Results.Channels[0].Alternatives) == 0 {
		return out, nil
	}
	ch := body.Results.Channels[0]
	alt := ch.Alternatives[0]
	out.Text = strings.TrimSpace(alt.Transcript)
	if p := strings.TrimSpace(alt.Paragraphs.Transcript); p != "" {
		out.Text = p
	}
	out.Confidence = alt.Confidence
	out.Language = ch.DetectedLanguage
	out.WordCount = len(alt.Words)
	if out.WordCount == 0 && out.Text != "" {
		out.WordCount = len(strings.Fields(out.Text))
	}
	return out, nil
}

// ExtractStream parses one live WS frame. Non-Results types yield a zero Result.
func ExtractStream(raw []byte) (Result, error) {
	var body streamResults
	if err := json.Unmarshal(raw, &body); err != nil {
		return Result{}, err
	}
	if !strings.EqualFold(body.Type, "Results") {
		return Result{}, nil
	}
	out := Result{
		IsFinal:     body.IsFinal,
		Duration:    body.Duration,
		RequestID:   body.Metadata.RequestID,
		Language:    body.Channel.DetectedLanguage,
	}
	if body.Metadata.Duration > out.Duration {
		out.Duration = body.Metadata.Duration
	}
	if len(body.Channel.Alternatives) == 0 {
		return out, nil
	}
	alt := body.Channel.Alternatives[0]
	out.Text = strings.TrimSpace(alt.Transcript)
	out.Confidence = alt.Confidence
	out.WordCount = len(alt.Words)
	if out.WordCount == 0 && out.Text != "" {
		out.WordCount = len(strings.Fields(out.Text))
	}
	return out, nil
}

// Accumulate folds a stream Result into running finals + display text.
func Accumulate(finals []string, partial Result) (nextFinals []string, display string) {
	nextFinals = finals
	if partial.IsFinal {
		if partial.Text != "" {
			nextFinals = append(append([]string{}, finals...), partial.Text)
		}
		return nextFinals, strings.TrimSpace(strings.Join(nextFinals, " "))
	}
	base := strings.TrimSpace(strings.Join(finals, " "))
	if partial.Text == "" {
		return nextFinals, base
	}
	if base == "" {
		return nextFinals, partial.Text
	}
	return nextFinals, base + " " + partial.Text
}
