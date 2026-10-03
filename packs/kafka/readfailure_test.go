package kafka

import (
	"testing"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A check's JSONPath that is not in an event's payload says so and names the
// properties the payload has (a table row like | JSONPath | value | taken for
// a property used to fail with a bare PathNotFoundException).
func TestReadFailureNamesThePayloadsProperties(t *testing.T) {
	doc, err := jsonx.Parse(`{"reference":"PX-1","sender":"shop","weightGrams":1840}`)
	if err != nil {
		t.Fatal(err)
	}
	_, _, rerr := jsonx.Read(doc, "JSONPath")
	if rerr == nil {
		t.Fatal("expected an error")
	}
	want := "JSONPath is not in the event's payload (its properties: reference, sender, weightGrams)"
	if got := readFailure(doc, "JSONPath", rerr); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
