package application

import (
	"errors"
	"testing"

	"github.com/matryer/is"
)

type registryTestModule struct{}

func (*registryTestModule) Start(*Context) error { return nil }
func (*registryTestModule) Stop(*Context) error  { return nil }

func TestModuleRegistry(t *testing.T) {
	tests := []struct {
		name string
		run  func(*is.I)
	}{
		{
			name: "registration freeze and lookup",
			run: func(is *is.I) {
				first := &registryTestModule{}
				second := &registryTestModule{}
				registry := new(moduleRegistry)

				is.NoErr(registry.add("first", first))                               // first unique module should register
				is.NoErr(registry.add("second", second))                             // second unique module should register
				is.True(errors.Is(registry.add("first", first), ErrDuplicateModule)) // duplicate names should be rejected

				snapshot := registry.freeze()
				is.Equal(len(snapshot), 2)                                                         // frozen snapshot should contain every registered module
				is.Equal(snapshot[0].name, "first")                                                // snapshot should preserve first registration order
				is.Equal(snapshot[1].name, "second")                                               // snapshot should preserve second registration order
				is.True(errors.Is(registry.add("third", &registryTestModule{}), ErrModulesFrozen)) // frozen registry should reject additions

				snapshot[0] = nil
				secondSnapshot := registry.snapshot()
				is.True(secondSnapshot[0] != nil)         // caller mutation should not alter the registry snapshot
				is.Equal(secondSnapshot[0].name, "first") // registry order should survive caller snapshot mutation

				got, found := registry.get("second")
				is.True(found)        // registered module lookup should report success
				is.Equal(got, second) // lookup should return the registered implementation
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.run(is.New(t))
		})
	}
}
