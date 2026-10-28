package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDBErrorIs500AndHidesCause(t *testing.T) {
	w := httptest.NewRecorder()
	dbError(w, "list clients", errors.New(`ERROR: column "notes" does not exist (SQLSTATE 42703)`))
	if w.Code != 500 {
		t.Fatalf("db failure must be 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "notes") || strings.Contains(w.Body.String(), "SQLSTATE") {
		t.Fatalf("raw db error leaked to client: %s", w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["code"] != "DB_ERROR" {
		t.Fatalf("want code DB_ERROR, got %q", body["code"])
	}
}
