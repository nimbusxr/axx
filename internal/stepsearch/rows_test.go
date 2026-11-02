package stepsearch

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestBriefNoteSaysHowValuesReadOnce(t *testing.T) {
	doc := core.ValueDoc(core.JSONValues, true)
	seen := map[string]bool{}
	first := BriefNote("A row is a property. "+doc, seen)
	second := BriefNote("A row is a field. "+doc, seen)
	if !strings.Contains(first, doc) {
		t.Errorf("the first note keeps it: %q", first)
	}
	if second != "A row is a field." {
		t.Errorf("the second leaves it out: %q", second)
	}
	if got := BriefNote(core.ValueDoc(core.TextValues, false), seen); got == "" {
		t.Error("a note of another kind of value still says how they read")
	}
}
