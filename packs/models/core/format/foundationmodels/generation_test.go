package foundationmodels

import (
	"strings"
	"testing"
)

func TestAStubWritesToolCallsAndRefusal(t *testing.T) {
	s, err := stubOf(t, deliveryNote,
		map[string]any{"name": "Deliver", "input": map[string]any{"text": "deliver"}, "output": map[string]any{"toolCalls": []any{map[string]any{"name": "markDelivered", "arguments": map[string]any{"reference": "PX-1"}}}}},
		map[string]any{"name": "Refuse", "input": map[string]any{"text": "secret"}, "output": map[string]any{"refusal": "I cannot help with that."}},
		otherwise,
	)
	if err != nil {
		t.Fatal(err)
	}
	b, err := write(s)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`("deliver", "[{\"arguments\":{\"reference\":\"PX-1\"},\"name\":\"markDelivered\"}]")`,
		`("secret", "I cannot help with that.")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the stub has no\n%s\nin\n%s", want, src)
		}
	}
}
