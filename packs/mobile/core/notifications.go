package mobilecore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// Notifications are the device's notifications, shown over the app.
type Notifications interface {
	// Shows reports whether a notification shows a text, in its title or its
	// text.
	Shows(ctx context.Context, text string) (bool, error)
	// Texts are what the notifications show, for a failure.
	Texts(ctx context.Context) []string
	// Close hides them again: the app is in front.
	Close(ctx context.Context) error
	// Open taps the notification that shows a text, as a person opens it: the device takes the
	// app to where the notification leads, and the notifications are gone.
	Open(ctx context.Context, text string) error
	// Act taps an action of the notification that shows a text: a button under it on Android
	// (opened out, if it is not), in the menu iOS shows when it is held.
	Act(ctx context.Context, text, action string) error
}

// ScreenNotifications are notifications the device's screen shows, which
// Read reads (Android's notification shade), until Hide hides them, and a
// finger taps where they are (Tap).
type ScreenNotifications struct {
	Read func(context.Context) (*Screen, error)
	Hide func(context.Context) error
	Tap  func(ctx context.Context, x, y float64) error
}

func (n ScreenNotifications) Shows(ctx context.Context, text string) (bool, error) {
	s, err := n.Read(ctx)
	return err == nil && s.Shows(text), err
}

func (n ScreenNotifications) Texts(ctx context.Context) []string {
	s, err := n.Read(ctx)
	if err != nil {
		return nil
	}
	return shownList(s)
}

func (n ScreenNotifications) Close(ctx context.Context) error { return n.Hide(ctx) }

// notification is the notification on the screen that shows a text: its own row (Android's
// expandableNotificationRow), else the first tappable node it is in.
func notification(s *Screen, text string) *Node {
	want := clean(text)
	for _, n := range s.Visible() {
		if !strings.Contains(clean(n.Text), want) && !strings.Contains(clean(n.Label), want) {
			continue
		}
		for p := n; p != nil; p = p.Parent {
			if p.Clickable || strings.HasSuffix(p.ID, ":id/expandableNotificationRow") {
				return p
			}
		}
	}
	return nil
}

// expandButton is the expand button of a node: its own, or that of the first node it is in that
// has one. A notification folded in the group of an app's notifications opens with the group's.
func expandButton(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		for _, d := range descendants(p) {
			if strings.HasSuffix(d.ID, ":id/expand_button") && d.Clickable {
				return d
			}
		}
	}
	return nil
}

func (n ScreenNotifications) tap(ctx context.Context, at *Node) error {
	return n.Tap(ctx, at.Bounds.X+at.Bounds.Width/2, at.Bounds.Y+at.Bounds.Height/2)
}

// Open taps the notification. One folded in the group of its app's notifications is not tappable
// until the group is opened out, as a person does first.
func (n ScreenNotifications) Open(ctx context.Context, text string) error {
	for opened := 0; ; opened++ {
		s, err := n.Read(ctx)
		if err != nil {
			return err
		}
		row := notification(s, text)
		if row == nil {
			return fmt.Errorf("no notification shows %q", text)
		}
		if row.Clickable {
			return n.tap(ctx, row)
		}
		expand := expandButton(row)
		if expand == nil || opened == 2 {
			return fmt.Errorf("the notification %q cannot be tapped", text)
		}
		if err := n.tap(ctx, expand); err != nil {
			return err
		}
	}
}

// Act taps a button of the notification. Android shows a notification's actions when it is open
// (the newest is): one that is not is opened out with its expand button, and its group's first
// when it is folded in one, as a person does.
func (n ScreenNotifications) Act(ctx context.Context, text, action string) error {
	for opened := 0; ; opened++ {
		s, err := n.Read(ctx)
		if err != nil {
			return err
		}
		row := notification(s, text)
		if row == nil {
			return fmt.Errorf("no notification shows %q", text)
		}
		for _, d := range descendants(row) {
			if strings.EqualFold(clean(d.Name()), clean(action)) {
				return n.tap(ctx, d)
			}
		}
		expand := expandButton(row)
		if expand == nil || opened == 3 {
			return fmt.Errorf("the notification %q has no action %q: %s", text, action, strings.Join(namesOf(descendants(row)), ", "))
		}
		if err := n.tap(ctx, expand); err != nil {
			return err
		}
	}
}

func descendants(n *Node) []*Node {
	var out []*Node
	for _, c := range n.Children {
		out = append(out, c)
		out = append(out, descendants(c)...)
	}
	return out
}

func namesOf(ns []*Node) []string {
	var out []string
	for _, n := range ns {
		if name := n.Name(); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// notificationLog is a device that keeps a log of what became of an app's notifications: a
// failed check shows it, to say why one did not show (the app never posted it, the device
// refused it, or it came and went).
type notificationLog interface {
	NotificationLog(ctx context.Context) string
}

// notificationSystem is a device that can say what its notification system was doing: when a
// notification the app posted never shows, it shows where it stopped.
type notificationSystem interface {
	NotificationSystem(ctx context.Context) string
}

// onNotification opens the device's notifications, waits for one that shows a text, and does
// what fn does with it (nothing, to check it is there); the notifications are closed after.
func onNotification(sc *core.Scenario, app, text string, wait time.Duration, fn func(ctx context.Context, n Notifications) error) error {
	return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
		n, err := d.OpenNotifications(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = n.Close(context.WithoutCancel(ctx)) }()
		ok, err := waitUntil(sc, wait, func() (bool, error) {
			ok, err := n.Shows(ctx, text)
			if appium.IsStale(err) {
				return false, nil // the notifications moved as they were read: look again
			}
			return ok, err
		})
		if err != nil {
			return err
		}
		if !ok {
			if l, ok := n.(notificationLog); ok {
				if log := l.NotificationLog(ctx); log != "" {
					// The whole log is attached; the step's log has its end, where the notification
					// went missing.
					sc.Attach("text/plain", []byte(log), "what the device of the "+app+" app logged of its notifications")
					sc.Log("what the device of the %s app logged of its notifications (the end of it):\n%s", app, lastLines(log, 30))
				}
			}
			if sys, ok := n.(notificationSystem); ok {
				if state := sys.NotificationSystem(ctx); state != "" {
					sc.Attach("text/plain", []byte(state), "what the notification system of the "+app+" app's device was doing")
				}
			}
			return core.Fail(fmt.Sprintf("The device of the %s app shows no notification %q", app, text), text,
				cloudstep.Shown("notifications", n.Texts(ctx), 30))
		}
		if fn == nil {
			return nil
		}
		return fn(ctx, n)
	})
}

// openNotification taps the notification that shows a text, as a person opens it.
func openNotification(sc *core.Scenario, app, text string) error {
	return onNotification(sc, app, secrets.Expand(sc, text), cloudstep.DefaultWait, func(ctx context.Context, n Notifications) error {
		if err := n.Open(ctx, text); err != nil {
			return fmt.Errorf("cannot open the notification %q: %w", text, err)
		}
		return nil
	})
}

func notificationSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.notification", Keyword: "Then", Since: since,
		Expr:     "[[within {duration} ]]the {word} app shows a notification {string}",
		TextArgs: []int{2},
		Doc: "Check that the device shows a notification with a text, in its title or its text: it opens the notifications " +
			"(Android's notification shade, iOS's Notification Center), looks, and closes them. It waits for the notification (10 seconds, or `within`).",
		Examples: []string{`Then the courier app shows a notification "PX-MOB-9401 delivered"`},
		Run: func(sc *core.Scenario, a core.Args) error {
			return onNotification(sc, a.String(1), secrets.Expand(sc, a.String(2)), cloudstep.Wait(a, 0), nil)
		},
	}, {
		ID: "mobile-core.notification-action", Keyword: "When", Since: since,
		Expr: "the {string} action of the {string} notification is tapped in the {word} app",
		Doc: "Tap one of a notification's actions, the notification found by a text in its title or its text: on Android a button " +
			"under it (opened out first, if it is not), on iOS in the menu a long press shows. It waits for the notification " +
			"(10 seconds). The notification itself, as a person opens it, is tapped with `the \"PX-MOB-9401 delivered\" notification " +
			"is tapped in the courier app`.",
		Examples: []string{`When the "Next delivery" action of the "PX-MOB-9401 delivered" notification is tapped in the courier app`},
		Run: func(sc *core.Scenario, a core.Args) error {
			action, text, app := secrets.Expand(sc, a.String(0)), secrets.Expand(sc, a.String(1)), a.String(2)
			return onNotification(sc, app, text, cloudstep.DefaultWait, func(ctx context.Context, n Notifications) error {
				if err := n.Act(ctx, text, action); err != nil {
					return fmt.Errorf("cannot tap the %q action of the notification %q: %w", action, text, err)
				}
				return nil
			})
		},
	}}
}

// lastLines are the last n lines of a log.
func lastLines(log string, n int) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
