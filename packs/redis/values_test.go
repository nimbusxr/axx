package redis

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule (ADR 0014): it fails before the
// step looks for a server, and text goes on to it. The integration test checks it
// against a key.
func TestAStepsOwnValue(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.ValueFails("the quote:DE-1 redis key has the value '690'", `690 reads as an integer, and only text fits here: write '"690"' for the text`)
	h.ValueFails("within 1s the quote:DE-1 redis key has the value 'true'", `true reads as a boolean, and only text fits here: write '"true"' for the text`)
	_ = h.Fails(`the quote:DE-1 redis key has the value '"690"'`, "No Redis server is registered in this scenario")
	_ = h.Fails("the quote:DE-1 redis key has the value 'online'", "No Redis server is registered in this scenario")
}
