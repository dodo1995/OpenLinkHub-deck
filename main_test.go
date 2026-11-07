package main

import (
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"
)

func TestDrawSVG(t *testing.T) {
	for _, tc := range []struct {
		pct             float64
		valid, charging bool
		color           string
	}{
		{62, true, false, "#22c55e"}, {40, true, false, "#f59e0b"}, {19, true, false, "#ef4444"}, {62, true, true, "#22c55e"}, {100, true, true, "#22c55e"}, {0, false, false, ""},
	} {
		s := draw(Device{Device: "K65 PLUS", Level: tc.pct}, tc.valid, Instance{}, tc.charging)
		b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "data:image/svg+xml;base64,"))
		if e != nil {
			t.Fatal(e)
		}
		var v any
		if e = xml.Unmarshal(b, &v); e != nil {
			t.Fatalf("invalid SVG at %v: %v, %s", tc.pct, e, b)
		}
		x := string(b)
		if strings.Contains(x, "%!") {
			t.Fatalf("fmt error at %v: %s", tc.pct, x)
		}
		if tc.valid && !strings.Contains(x, tc.color) {
			t.Errorf("missing color %s at %v", tc.color, tc.pct)
		}
		hasArrow := strings.Contains(x, "stroke=\"#000000\"")
		if hasArrow != (tc.charging && tc.valid && tc.pct < 100) {
			t.Errorf("arrow mismatch at %v", tc.pct)
		}
		if !tc.valid && !strings.Contains(x, "Offline") {
			t.Error("missing offline text")
		}
	}
}
