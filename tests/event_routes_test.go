package tests

import (
	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"reflect"
	"slices"
	"testing"
)

func TestEventTopologyMatchesEveryModule(t *testing.T) {
	for _, name := range bootstrap.Names {
		module, e := bootstrap.New(name, platform.Dependencies{})
		if e != nil {
			t.Fatal(e)
		}
		actual, want := append([]string(nil), module.Topics...), platform.EventTopics(name)
		slices.Sort(actual)
		slices.Sort(want)
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s topics differ from broker topology: %v vs %v", name, actual, want)
		}
	}
}
