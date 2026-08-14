// SPDX-License-Identifier: Apache-2.0
package deepgram

import "testing"

func TestExtractPrerecordedFromDocsShape(t *testing.T) {
	raw := []byte(`{
	  "metadata": {"request_id": "2479c8c8-8185-40ac-9ac6-f0874419f793", "duration": 25.9},
	  "results": {"channels": [{"detected_language": "en", "alternatives": [{
	    "transcript": "Yeah. As as much as, it's worth celebrating.",
	    "confidence": 0.999,
	    "words": [{"word": "yeah"}, {"word": "as"}]
	  }]}]}
	}`)
	got, err := ExtractPrerecorded(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Yeah. As as much as, it's worth celebrating." {
		t.Fatalf("text = %q", got.Text)
	}
	if got.RequestID != "2479c8c8-8185-40ac-9ac6-f0874419f793" || got.Language != "en" || got.WordCount != 2 {
		t.Fatalf("meta = %+v", got)
	}
}

func TestExtractPrerecordedPrefersParagraphs(t *testing.T) {
	raw := []byte(`{
	  "metadata": {"request_id": "p1", "duration": 8},
	  "results": {"channels": [{"detected_language": "ru", "alternatives": [{
	    "transcript": "one wall of text",
	    "confidence": 0.9,
	    "words": [{"word": "one"}, {"word": "wall"}],
	    "paragraphs": {"transcript": "\nfirst paragraph.\n\nsecond paragraph."}
	  }]}]}
	}`)
	got, err := ExtractPrerecorded(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "first paragraph.\n\nsecond paragraph." {
		t.Fatalf("text = %q", got.Text)
	}
	if got.Language != "ru" || got.WordCount != 2 {
		t.Fatalf("meta = %+v", got)
	}
}

func TestExtractStreamResults(t *testing.T) {
	raw := []byte(`{
	  "type": "Results",
	  "is_final": true,
	  "duration": 1.2,
	  "metadata": {"request_id": "req-1"},
	  "channel": {"alternatives": [{"transcript": "hello world", "confidence": 0.9, "words": [{}, {}]}]}
	}`)
	got, err := ExtractStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello world" || !got.IsFinal || got.RequestID != "req-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractStreamIgnoresMetadataFrame(t *testing.T) {
	got, err := ExtractStream([]byte(`{"type":"Metadata","request_id":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || got.IsFinal {
		t.Fatalf("expected empty non-result, got %+v", got)
	}
}

func TestAccumulateInterimThenFinal(t *testing.T) {
	var finals []string
	finals, display := Accumulate(finals, Result{Text: "hel", IsFinal: false})
	if display != "hel" {
		t.Fatalf("interim = %q", display)
	}
	finals, display = Accumulate(finals, Result{Text: "hello", IsFinal: true})
	if display != "hello" || len(finals) != 1 {
		t.Fatalf("final = %q %v", display, finals)
	}
	_, display = Accumulate(finals, Result{Text: "there", IsFinal: false})
	if display != "hello there" {
		t.Fatalf("second interim = %q", display)
	}
}
