// SPDX-License-Identifier: Apache-2.0
package stats

import "testing"

func TestShouldCountOnlySuccessfulNonEmpty(t *testing.T) {
	cases := []struct {
		status, text string
		want         bool
	}{
		{"sent", "hello", true},
		{"sent", "  ", false},
		{"sent", "", false},
		{"empty", "hello", false},
		{"failed", "hello", false},
		{"received", "hello", false},
	}
	for _, tc := range cases {
		if got := ShouldCount(tc.status, tc.text); got != tc.want {
			t.Fatalf("ShouldCount(%q,%q)=%v want %v", tc.status, tc.text, got, tc.want)
		}
	}
}
