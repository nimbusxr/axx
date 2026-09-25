package cloudstep

import "testing"

func TestColon(t *testing.T) {
	for in, want := range map[string]string{
		"sqs queue":               "sqs queue:",
		"service bus queue/topic": "service bus queue:/topic:",
	} {
		if got := colon(in); got != want {
			t.Errorf("colon(%q) = %q, want %q", in, got, want)
		}
	}
}
