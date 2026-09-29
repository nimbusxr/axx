package mobilecore

import (
	"context"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
)

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
				screen, closeThem, err := d.OpenNotifications(ctx)
				if err != nil {
					return err
				}
				defer func() { _ = closeThem(context.WithoutCancel(ctx)) }()
				ok, err := waitUntil(sc, wait, func() (bool, error) {
					s, err := screen(ctx)
					return err == nil && s.Shows(text), err
				})
				if err != nil || ok {
					return err
				}
				return core.Failf("The device of the %s app shows no notification %q", app, text)
			})
		},
	}}
}
