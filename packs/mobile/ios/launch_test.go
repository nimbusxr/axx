package mobileios

import (
	"context"
	"reflect"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestLaunchLibrariesComposeAndReload(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
	other := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
	n := 0
	OnLaunch(sc, func(l *Launch) error {
		n++
		l.InsertLibrary("/models.dylib")
		l.InsertLibrary("/models.dylib")
		l.InsertLibrary("/foundation.dylib")
		return nil
	})
	for range 2 {
		l := &Launch{Environment: map[string]string{"DYLD_INSERT_LIBRARIES": "/ar.dylib", "AXX_AR_LIBRARY": "/ar.dylib"}}
		l.InsertLibrary("/simulator.dylib")
		if err := prepareLaunch(sc, l); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"DYLD_INSERT_LIBRARIES": "/ar.dylib:/simulator.dylib:/models.dylib:/foundation.dylib", "AXX_AR_LIBRARY": "/ar.dylib"}
		if !reflect.DeepEqual(l.Environment, want) {
			t.Fatalf("environment: %v", l.Environment)
		}
	}
	if err := prepareLaunch(other, &Launch{Environment: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("hooks ran %d times", n)
	}
}
