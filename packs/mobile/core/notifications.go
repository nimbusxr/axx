package mobilecore

import (
	"context"
	"fmt"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
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
}

// ScreenNotifications are notifications the device's screen shows, which
// Read reads (Android's notification shade), until Hide hides them.
type ScreenNotifications struct {
	Read func(context.Context) (*Screen, error)
	Hide func(context.Context) error
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

func notificationSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.notification", Keyword: "Then", Since: since,
		Expr: "[[within {duration} ]]the {word} app shows a notification {string}",
		Doc: "Check that the device shows a notification with a text, in its title or its text: it opens the notifications " +
			"(Android's notification shade, iOS's Notification Center), looks, and closes them. It waits for the notification (10 seconds, or `within`).",
		Examples: []string{`Then the courier app shows a notification "PX-MOB-9401 delivered"`},
		Run: func(sc *core.Scenario, a core.Args) error {
			wait, app, text := cloudstep.Wait(a, 0), a.String(1), secrets.Expand(sc, a.String(2))
			return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
				n, err := d.OpenNotifications(ctx)
				if err != nil {
					return err
				}
				defer func() { _ = n.Close(context.WithoutCancel(ctx)) }()
				ok, err := waitUntil(sc, wait, func() (bool, error) { return n.Shows(ctx, text) })
				if err != nil || ok {
					return err
				}
				return core.Fail(fmt.Sprintf("The device of the %s app shows no notification %q", app, text), text,
					cloudstep.Shown("notifications", n.Texts(ctx), 30))
			})
		},
	}}
}
