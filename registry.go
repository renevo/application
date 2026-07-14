package application

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

type registeredModule struct {
	name            string
	implementation  Module
	state           moduleState
	startWasEntered bool
}

type moduleRegistry struct {
	mu      sync.RWMutex
	ordered []*registeredModule
	byName  map[string]*registeredModule
	frozen  bool
}

func (r *moduleRegistry) add(name string, implementation Module) error {
	if name == "" {
		return errors.New("module name must not be empty")
	}
	if isNil(implementation) {
		return errors.New("module must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.frozen {
		return ErrModulesFrozen
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateModule, name)
	}
	if r.byName == nil {
		r.byName = make(map[string]*registeredModule)
	}

	module := &registeredModule{
		name:           name,
		implementation: implementation,
		state:          moduleStateRegistered,
	}
	r.byName[name] = module
	r.ordered = append(r.ordered, module)
	return nil
}

func (r *moduleRegistry) freeze() []*registeredModule {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.frozen = true
	return append([]*registeredModule(nil), r.ordered...)
}

func (r *moduleRegistry) get(name string) (Module, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	module, found := r.byName[name]
	if !found {
		return nil, false
	}
	return module.implementation, true
}

func (r *moduleRegistry) snapshot() []*registeredModule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*registeredModule(nil), r.ordered...)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
