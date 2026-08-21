// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"encoding/json"
	"strings"
)

// Result is the normalized transcript we persist and show.
type Result struct {
	Text       string
	Confidence float64
	Language   string
	Duration   float64
	RequestID  string
	WordCount  int
	Turns      []Turn
}

// Turn is one continuous speaker segment from a diarized transcript.
type Turn struct {
	Speaker int    `json:"speaker"` // Deepgram numbers speakers from 0
	Text    string `json:"text"`
}

type prerecordedResponse struct {
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
					Paragraphs []struct {
						Speaker   int `json:"speaker"`
						Sentences []struct {
							Text string `json:"text"`
						} `json:"sentences"`
					} `json:"paragraphs"`
				} `json:"paragraphs"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// ExtractPrerecorded parses a Listen REST body.
func ExtractPrerecorded(raw []byte) (Result, error) {
	var body prerecordedResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return Result{}, err
	}
	out := Result{RequestID: body.Metadata.RequestID, Duration: body.Metadata.Duration}
	if len(body.Results.Channels) == 0 || len(body.Results.Channels[0].Alternatives) == 0 {
		return out, nil
	}
	ch := body.Results.Channels[0]
	alt := ch.Alternatives[0]
	out.Text = strings.TrimSpace(alt.Transcript)
	if p := strings.TrimSpace(alt.Paragraphs.Transcript); p != "" {
		out.Text = p
	}
	if turns := turnsFrom(alt.Paragraphs.Paragraphs); len(turns) > 1 {
		out.Turns = turns
	}
	out.Confidence = alt.Confidence
	out.Language = ch.DetectedLanguage
	out.WordCount = len(alt.Words)
	if out.WordCount == 0 && out.Text != "" {
		out.WordCount = len(strings.Fields(out.Text))
	}
	return out, nil
}

// turnsFrom folds speaker-tagged sentences into consecutive speaker turns.
func turnsFrom(paragraphs []struct {
	Speaker   int `json:"speaker"`
	Sentences []struct {
		Text string `json:"text"`
	} `json:"sentences"`
}) []Turn {
	var turns []Turn
	seen := map[int]bool{}
	for _, p := range paragraphs {
		var sentenceText []string
		for _, s := range p.Sentences {
			text := strings.TrimSpace(s.Text)
			if text == "" {
				continue
			}
			sentenceText = append(sentenceText, text)
		}
		text := strings.Join(sentenceText, " ")
		if text == "" {
			continue
		}
		seen[p.Speaker] = true
		if n := len(turns); n > 0 && turns[n-1].Speaker == p.Speaker {
			turns[n-1].Text = strings.TrimSpace(turns[n-1].Text + " " + text)
			continue
		}
		turns = append(turns, Turn{Speaker: p.Speaker, Text: text})
	}
	if len(seen) < 2 {
		return nil
	}
	return turns
}
