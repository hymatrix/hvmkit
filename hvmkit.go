// Package hvmkit builds hymx VMs from explicitly registered callbacks.
// Applications own their business state, persistence and synchronization.
package hvmkit

import (
	"errors"
	"fmt"
	"strings"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

var (
	ErrInvalidConfig = errors.New("hvmkit: invalid configuration")
	ErrAppFrozen     = errors.New("hvmkit: app is frozen")
	ErrUnknownAction = errors.New("hvmkit: unknown action")
)

// Handler is an Action callback using the native hymx request and result types.
type Handler = func(from string, meta vmmSchema.Meta) vmmSchema.Result

// BeforeApplyHandler returns handled=true to stop processing and return its Result.
type BeforeApplyHandler = func(from string, meta vmmSchema.Meta) (vmmSchema.Result, bool)

// App registers callbacks for a single VM. Its zero value is ready to use.
// It must not be copied or used concurrently. Build reports registration errors
// and freezes the App, even on failure.
type App struct {
	before                              []BeforeApplyHandler
	actions                             map[string]Handler
	checkpoint                          func() (string, error)
	restore                             func(string) error
	close                               func() error
	checkpointSet, restoreSet, closeSet bool
	stateless                           bool
	frozen                              bool
	errs                                []error
}

// New creates an App for explicit callback registration.
func New() *App { return &App{} }

func (app *App) mutable() {
	if app.frozen {
		panic(ErrAppFrozen)
	}
}

func (app *App) invalid(format string, args ...any) {
	app.errs = append(app.errs, fmt.Errorf(format, args...))
}

// BeforeApply appends callbacks in registration order, before Action lookup.
// A false handled value ignores the Result and continues to the next callback.
func (app *App) BeforeApply(handlers ...BeforeApplyHandler) {
	app.mutable()
	for _, handler := range handlers {
		if handler == nil {
			app.invalid("nil BeforeApply callback")
			continue
		}
		app.before = append(app.before, handler)
	}
}

// Action registers an exact, case-sensitive name. Names must not be blank.
func (app *App) Action(name string, handler Handler) {
	app.mutable()
	if strings.TrimSpace(name) == "" {
		app.invalid("action name must not be blank")
		return
	}
	if _, exists := app.actions[name]; exists {
		app.invalid("duplicate action %q", name)
		return
	}
	if app.actions == nil {
		app.actions = make(map[string]Handler)
	}
	app.actions[name] = handler
	if handler == nil {
		app.invalid("action %q has a nil handler", name)
	}
}

// Checkpoint registers the callback that saves complete business state.
func (app *App) Checkpoint(fn func() (string, error)) {
	app.mutable()
	if app.checkpointSet {
		app.invalid("duplicate Checkpoint registration")
		return
	}
	app.checkpointSet, app.checkpoint = true, fn
	if fn == nil {
		app.invalid("nil Checkpoint callback")
	}
}

// Restore registers the callback that restores complete business state.
func (app *App) Restore(fn func(string) error) {
	app.mutable()
	if app.restoreSet {
		app.invalid("duplicate Restore registration")
		return
	}
	app.restoreSet, app.restore = true, fn
	if fn == nil {
		app.invalid("nil Restore callback")
	}
}

// Close registers optional resource cleanup. Register it even when another
// registration is invalid so Build can release resources on failure.
func (app *App) Close(fn func() error) {
	app.mutable()
	if app.closeSet {
		app.invalid("duplicate Close registration")
		return
	}
	app.closeSet, app.close = true, fn
	if fn == nil {
		app.invalid("nil Close callback")
	}
}

// Stateless declares that the entire VM has no business state. Repeated calls
// are harmless. Custom Checkpoint and Restore callbacks are not allowed.
func (app *App) Stateless() {
	app.mutable()
	app.stateless = true
}

// Build freezes the App and validates configuration. It may be called only once.
// On failure it calls the registered Close callback and joins any cleanup error.
func (app *App) Build() (vmmSchema.Vm, error) {
	if app.frozen {
		return nil, ErrAppFrozen
	}
	app.frozen = true
	errs := app.errs
	if app.stateless {
		if app.checkpointSet || app.restoreSet {
			errs = append(errs, errors.New("Stateless conflicts with Checkpoint/Restore"))
		}
	} else if !app.checkpointSet || !app.restoreSet {
		errs = append(errs, errors.New("stateful VM requires both Checkpoint and Restore"))
	}
	if len(errs) > 0 {
		err := errors.Join(append([]error{ErrInvalidConfig}, errs...)...)
		if app.close != nil {
			err = errors.Join(err, app.close())
		}
		return nil, err
	}
	vm := &machine{
		before:     app.before,
		actions:    app.actions,
		checkpoint: app.checkpoint,
		restore:    app.restore,
		close:      app.close,
	}
	if app.stateless {
		vm.checkpoint = func() (string, error) { return "", nil }
		vm.restore = func(string) error { return nil }
	}
	if vm.close == nil {
		vm.close = func() error { return nil }
	}
	return vm, nil
}

type machine struct {
	before     []BeforeApplyHandler
	actions    map[string]Handler
	checkpoint func() (string, error)
	restore    func(string) error
	close      func() error
}

var _ vmmSchema.Vm = (*machine)(nil)

func (vm *machine) Apply(from string, meta vmmSchema.Meta) vmmSchema.Result {
	for _, before := range vm.before {
		if result, handled := before(from, meta); handled {
			return result
		}
	}
	handler, ok := vm.actions[meta.Action]
	if !ok {
		return vmmSchema.Result{Error: fmt.Errorf("%w: %q", ErrUnknownAction, meta.Action)}
	}
	return handler(from, meta)
}

func (vm *machine) Checkpoint() (string, error) { return vm.checkpoint() }
func (vm *machine) Restore(data string) error   { return vm.restore(data) }
func (vm *machine) Close() error                { return vm.close() }
