package mobilecore

import "testing"

func TestAnUnknownBackgroundTaskNamesTheAppsTasks(t *testing.T) {
	for _, c := range []struct {
		tasks []string
		want  string
	}{
		{[]string{"round-reminder", "depot-sync"}, `the courier app has no background task "round-reminders": its background tasks are "depot-sync", "round-reminder"`},
		{nil, `the courier app has no background task "round-reminders": it has none`},
	} {
		if got := UnknownBackgroundTask("courier", "round-reminders", c.tasks).Error(); got != c.want {
			t.Errorf("got  %s\nwant %s", got, c.want)
		}
	}
}
