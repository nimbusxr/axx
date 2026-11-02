package cli

import "testing"

// A value in a step's own text follows the rule (ADR 0014); a command line is
// the shell's, and a pattern a pattern.
func TestAStepsOwnValue(t *testing.T) {
	t.Setenv("PARCEL_NUMBER", "6101")
	h := harness(t)
	h.File("labels/PX-ADM-6101.zpl", zpl)
	h.OK("the admin command is run with 'label PX-ADM-6101'")
	h.ValueFails("the admin command's output contains '6101'", `6101 reads as an integer, and only text fits here: write '"6101"' for the text`)
	h.ValueFails(`the admin command's output contains "6101"`, `6101 reads as an integer, and only text fits here: write '"6101"' for the text`)
	h.ValueFails("the admin command's error output contains 'null'", `null reads as null, and only text fits here: write '"null"' for the text`)
	h.OK(`the admin command's output contains '"6101"'`)
	h.OK("the admin command's output contains 'PX-ADM-6101'")
	h.OK("the admin command's output contains '${env:PARCEL_NUMBER}'") // a reference is text, whatever it expands to
	h.OK(`the admin command's output has a line matching '6101'`)
	_ = h.Fails(`the admin command's output contains '"null"'`, `does not contain "null"`)
}
