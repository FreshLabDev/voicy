// SPDX-License-Identifier: Apache-2.0
package settings

import (
	"os"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	got := Default()
	want := Settings{SmartFormat: true, Paragraphs: true, Quote: true}
	if got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
}

func TestDefaultVariantMatchesPreSettingsOptions(t *testing.T) {
	if got := Default().Variant(); got != DefaultVariant {
		t.Fatalf("Default().Variant() = %q, want %q", got, DefaultVariant)
	}
}

func TestVariantChangesOnlyWithSTTOptions(t *testing.T) {
	base := Default().Variant()
	deliveryOnly := Default()
	deliveryOnly.Quote = false
	deliveryOnly.Meta = true
	if got := deliveryOnly.Variant(); got != base {
		t.Fatalf("delivery-only changes must not fork the cache: %q vs %q", got, base)
	}
	stt := Default()
	stt.Diarize = true
	if got := stt.Variant(); got == base {
		t.Fatal("diarize must change the variant")
	}
}

func TestSpecOf(t *testing.T) {
	for _, spec := range Specs {
		if got, ok := SpecOf(spec.Key); !ok || got != spec {
			t.Fatalf("SpecOf(%q) = %+v, %v", spec.Key, got, ok)
		}
	}
	if _, ok := SpecOf("nope"); ok {
		t.Fatal("unknown key must not resolve")
	}
}

func TestApplyRejectsUnknownKey(t *testing.T) {
	if _, err := (Settings{}).Apply("nope", true); err == nil {
		t.Fatal("expected error for unknown key")
	}
	s := Settings{}
	got, err := s.Apply("diarize", true)
	if err != nil || !got.Diarize || s.Diarize {
		t.Fatalf("apply = %+v, err = %v; original must be unchanged", got, err)
	}
}

func TestIsOn(t *testing.T) {
	s := Settings{SmartFormat: true}
	if !s.IsOn("smart_format") {
		t.Fatal("smart_format should be on")
	}
	if s.IsOn("diarize") {
		t.Fatal("diarize should be off")
	}
	if s.IsOn("nope") {
		t.Fatal("unknown key must report off")
	}
}

func TestKeysMatchSpecs(t *testing.T) {
	keys := Keys()
	if len(keys) != len(Specs) {
		t.Fatalf("keys = %v", keys)
	}
	for i, spec := range Specs {
		if keys[i] != spec.Key {
			t.Fatalf("keys[%d] = %q, want %q", i, keys[i], spec.Key)
		}
	}
}

// The migration's column defaults must match the Go specs: a fresh
// user_settings row has to equal Default() field by field.
func TestMigrationColumnsMatchSpecs(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/002_user_settings.sql")
	if err != nil {
		t.Skip("migration file not readable from test")
	}
	sql := string(raw)
	for _, spec := range Specs {
		def := "false"
		if spec.Default {
			def = "true"
		}
		want := spec.Key + " BOOLEAN NOT NULL DEFAULT " + def
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing column default %q", want)
		}
	}
	if !strings.Contains(sql, "'"+DefaultVariant+"'") {
		t.Fatalf("migration variant default must embed %q", DefaultVariant)
	}
}

// Diarization is requested with paragraphs because Deepgram returns speaker
// turns on paragraph objects. The variant has to describe that request, or the
// cache row claims an option set the transcript was never produced with.
func TestVariantReflectsDiarizeImpliesParagraphs(t *testing.T) {
	s := Settings{SmartFormat: true, Diarize: true}
	if got := s.Normalized(); !got.Paragraphs {
		t.Fatalf("normalized = %+v", got)
	}
	if got, want := s.Variant(), "sf1-p1-fw0-pf0-d1"; got != want {
		t.Fatalf("variant = %q, want %q", got, want)
	}
	if got, want := Default().Variant(), DefaultVariant; got != want {
		t.Fatalf("default variant = %q, want %q", got, want)
	}
}
