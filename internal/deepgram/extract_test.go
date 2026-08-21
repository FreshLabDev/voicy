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

func TestExtractPrerecordedDiarizes(t *testing.T) {
	raw := []byte(`{
	  "metadata": {"request_id": "d1", "duration": 12},
	  "results": {"channels": [{"detected_language": "en", "alternatives": [{
	    "transcript": "one big wall of text",
	    "confidence": 0.9,
	    "words": [],
	    "paragraphs": {"transcript": "Hello there. How are you? I am fine.", "paragraphs": [
	      {"speaker": 0, "sentences": [{"text": "Hello there."}, {"text": "How are you?"}]},
	      {"speaker": 1, "sentences": [{"text": "I am fine."}]}
	    ]}
	  }]}]}
	}`)
	got, err := ExtractPrerecorded(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Hello there. How are you? I am fine." {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Turns) != 2 || got.Turns[0].Speaker != 0 || got.Turns[0].Text != "Hello there. How are you?" || got.Turns[1].Speaker != 1 {
		t.Fatalf("turns = %+v", got.Turns)
	}
}

func TestExtractPrerecordedSingleSpeakerNoLabels(t *testing.T) {
	raw := []byte(`{
	  "metadata": {"request_id": "d2", "duration": 5},
	  "results": {"channels": [{"detected_language": "en", "alternatives": [{
	    "transcript": "wall",
	    "paragraphs": {"transcript": "All mine. Every word.", "paragraphs": [
	      {"speaker": 0, "sentences": [{"text": "All mine."}, {"text": "Every word."}]}
	    ]}
	  }]}]}
	}`)
	got, err := ExtractPrerecorded(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "All mine. Every word." || len(got.Turns) != 0 {
		t.Fatalf("result = %+v", got)
	}
}
