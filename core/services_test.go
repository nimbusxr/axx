package core

import "testing"

// A registry that knows the step registering a service says how to add a
// missing one, by name or as the default.
func TestServicesSayHowToRegisterAMissingOne(t *testing.T) {
	const expr = "the {word} service with the following properties:"
	r := NewServices[int]("Service", "").RegisteredBy(expr)
	if _, err := r.Default(); err == nil || err.Error() != `No service set; register one with "`+expr+`"` {
		t.Errorf("default: %v", err)
	}
	if _, err := r.Get("parcels"); err == nil || err.Error() != `Service "parcels" not set; register it with "`+expr+`"` {
		t.Errorf("named: %v", err)
	}
}

// A message that already names the step does not name it twice.
func TestServicesDoNotRepeatTheRegisteringStep(t *testing.T) {
	const expr = "the {word} mailbox with the following properties:"
	empty := `No mailbox is registered in this scenario; register one with "` + expr + `"`
	r := NewServices[int]("Mailbox", empty).RegisteredBy(expr)
	if _, err := r.Default(); err == nil || err.Error() != empty {
		t.Errorf("default: %v", err)
	}
}

// Without RegisteredBy the messages stay as they were.
func TestServicesWithoutARegisteringStep(t *testing.T) {
	r := NewServices[int]("Database service", "No database services set")
	if _, err := r.Default(); err == nil || err.Error() != "No database services set" {
		t.Errorf("default: %v", err)
	}
	if _, err := r.Get("x"); err == nil || err.Error() != `Database service "x" not set` {
		t.Errorf("named: %v", err)
	}
}
