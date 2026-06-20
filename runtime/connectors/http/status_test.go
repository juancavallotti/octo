package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		name string
		set  any
		want int
	}{
		{"absent", nil, http.StatusOK},
		{"valid float", float64(201), http.StatusCreated},
		{"valid int", 404, http.StatusNotFound},
		{"below range", float64(99), http.StatusOK},
		{"above range", float64(600), http.StatusOK},
		{"not a number", "nope", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := types.NewMessage("")
			if err != nil {
				t.Fatalf("NewMessage: %v", err)
			}
			if tt.set != nil {
				msg.Variables.Set(httpStatusVar, tt.set)
			}
			if got := statusFor(msg, http.StatusOK); got != tt.want {
				t.Errorf("statusFor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWriteResultUsesHTTPStatusVar(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"ok": false}
	msg.Variables.Set(httpStatusVar, float64(http.StatusBadRequest))

	rec := httptest.NewRecorder()
	(&source{}).writeResult(rec, result{kind: types.FlowEventCompleted, msg: msg})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
