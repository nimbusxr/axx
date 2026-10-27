package task

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func TestGenerateToolCallsAndRefusal(t *testing.T) {
	c := &contract.Contract{Task: "text-generation"}
	for _, tc := range []struct {
		name, answer, want string
	}{
		{"tool calls", `{"toolCalls":[{"name":"markDelivered","arguments":{"reference":"PX-1"}}]}`, `[{"name":"markDelivered","arguments":{"reference":"PX-1"}}]`},
		{"refusal", `{"refusal":"I cannot help with that."}`, "I cannot help with that."},
		{"empty tool calls", `{"toolCalls":[]}`, "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Text(c, json.RawMessage(tc.answer))
			if err != nil || got != tc.want {
				t.Errorf("Text(%s) = %q, %v; want %q", tc.answer, got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name, answer, want string
	}{
		{"two answers", `{"text":"one","refusal":"no"}`, "exactly one"},
		{"no answer", `{}`, "gives no"},
		{"calls are not an array", `{"toolCalls":{"name":"markDelivered"}}`, "not an array"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Text(c, json.RawMessage(tc.answer)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Text(%s): error %v, want one containing %q", tc.answer, err, tc.want)
			}
		})
	}
}
