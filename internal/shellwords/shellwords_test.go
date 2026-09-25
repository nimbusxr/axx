package shellwords

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := map[string][]string{
		`./gradlew bootRun`:                    {"./gradlew", "bootRun"},
		`sh -c "docker compose up && echo ok"`: {"sh", "-c", "docker compose up && echo ok"},
		`  a   b  `:                            {"a", "b"},
		`echo ""`:                              {"echo", ""},
		``:                                     nil,
	}
	for in, want := range cases {
		if got := Split(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Split(%q) = %q, want %q", in, got, want)
		}
	}
}
