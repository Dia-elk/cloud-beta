package apperr

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestEveryCodeIsWellFormedAndTrilingual(t *testing.T) {
	format := regexp.MustCompile(`^[A-Z]+_\d{3}$`)
	for code, d := range All() {
		if !format.MatchString(string(code)) {
			t.Errorf("%s: code must match SERVICE_NNN", code)
		}
		if d.Status < 400 || d.Status > 599 {
			t.Errorf("%s: status %d is not an error status", code, d.Status)
		}
		if d.EN == "" || d.FR == "" || d.AR == "" {
			t.Errorf("%s: every code needs EN, FR and AR messages", code)
		}
	}
}

func TestUnknownCodeFallsBackToInternal(t *testing.T) {
	status, body := Lookup("NOPE_999")
	if status != 500 || body.Code != GatewayInternal {
		t.Fatalf("unknown code must fall back to GATEWAY_000/500, got %d %s", status, body.Code)
	}
}

func TestEveryCodeIsDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/ERROR_CODES.md")
	if err != nil {
		t.Fatalf("read docs/ERROR_CODES.md: %v", err)
	}
	for code := range All() {
		if !strings.Contains(string(doc), "`"+string(code)+"`") {
			t.Errorf("%s is not documented in docs/ERROR_CODES.md", code)
		}
	}
}
