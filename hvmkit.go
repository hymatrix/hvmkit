// Package hvmkit builds hymx VMs from explicitly registered callbacks.
// Applications own their business state, persistence and synchronization.
package hvmkit

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

var (
	ErrInvalidConfig = errors.New("hvmkit: invalid configuration")
	ErrBuilderFrozen = errors.New("hvmkit: builder is frozen")
	ErrUnknownAction = errors.New("hvmkit: unknown action")
)

// Handler is an Action callback using the native hymx request and result types.
type Handler = func(from string, meta vmmSchema.Meta) vmmSchema.Result

// Builder collects callbacks for a single VM. Its zero value is ready to use.
// It must not be copied or used concurrently. Registration errors are reported
// by Build. After a successful Build, registration methods panic with
// ErrBuilderFrozen and subsequent Build calls return ErrBuilderFrozen.
type Builder struct {
	actions                             map[string]Handler
	checkpoint                          func() (string, error)
	restore                             func(string) error
	close                               func() error
	checkpointSet, restoreSet, closeSet bool
	stateless                           bool
	frozen                              bool
	errs                                []error
}

// New creates a Builder for explicit callback registration.
func New() *Builder { return &Builder{} }

func (b *Builder) mutable() {
	if b.frozen {
		panic(ErrBuilderFrozen)
	}
}

func (b *Builder) invalid(format string, args ...any) {
	b.errs = append(b.errs, fmt.Errorf(format, args...))
}

// Action registers an exact, case-sensitive name. Names must not be blank.
func (b *Builder) Action(name string, handler Handler) {
	b.mutable()
	if strings.TrimSpace(name) == "" {
		b.invalid("action name must not be blank")
		return
	}
	if _, exists := b.actions[name]; exists {
		b.invalid("duplicate action %q", name)
		return
	}
	if b.actions == nil {
		b.actions = make(map[string]Handler)
	}
	b.actions[name] = handler
	if handler == nil {
		b.invalid("action %q has a nil handler", name)
	}
}

// Checkpoint registers the callback that saves complete business state.
func (b *Builder) Checkpoint(fn func() (string, error)) {
	b.mutable()
	if b.checkpointSet {
		b.invalid("duplicate Checkpoint registration")
		return
	}
	b.checkpointSet, b.checkpoint = true, fn
	if fn == nil {
		b.invalid("nil Checkpoint callback")
	}
}

// Restore registers the callback that restores complete business state.
func (b *Builder) Restore(fn func(string) error) {
	b.mutable()
	if b.restoreSet {
		b.invalid("duplicate Restore registration")
		return
	}
	b.restoreSet, b.restore = true, fn
	if fn == nil {
		b.invalid("nil Restore callback")
	}
}

// Close registers optional resource cleanup. Register it even when another
// registration is invalid so Factory can release resources on Build failure.
func (b *Builder) Close(fn func() error) {
	b.mutable()
	if b.closeSet {
		b.invalid("duplicate Close registration")
		return
	}
	b.closeSet, b.close = true, fn
	if fn == nil {
		b.invalid("nil Close callback")
	}
}

// Stateless declares that the entire VM has no business state. Repeated calls
// are harmless. Custom Checkpoint and Restore callbacks are not allowed.
func (b *Builder) Stateless() {
	b.mutable()
	b.stateless = true
}

// Build validates configuration and freezes the Builder on success.
// A failed Build does not release resources; Factory handles that cleanup.
func (b *Builder) Build() (vmmSchema.Vm, error) {
	if b.frozen {
		return nil, ErrBuilderFrozen
	}
	errs := append([]error(nil), b.errs...)
	if b.stateless {
		if b.checkpointSet || b.restoreSet {
			errs = append(errs, errors.New("Stateless conflicts with Checkpoint/Restore"))
		}
	} else if !b.checkpointSet || !b.restoreSet {
		errs = append(errs, errors.New("stateful VM requires both Checkpoint and Restore"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(append([]error{ErrInvalidConfig}, errs...)...)
	}
	vm := &machine{
		actions:    maps.Clone(b.actions),
		checkpoint: b.checkpoint,
		restore:    b.restore,
		close:      b.close,
	}
	if b.stateless {
		vm.checkpoint = func() (string, error) { return "", nil }
		vm.restore = func(string) error { return nil }
	}
	if vm.close == nil {
		vm.close = func() error { return nil }
	}
	b.frozen = true
	return vm, nil
}

type machine struct {
	actions    map[string]Handler
	checkpoint func() (string, error)
	restore    func(string) error
	close      func() error
}

var _ vmmSchema.Vm = (*machine)(nil)

func (vm *machine) Apply(from string, meta vmmSchema.Meta) vmmSchema.Result {
	handler, ok := vm.actions[meta.Action]
	if !ok {
		return vmmSchema.Result{Error: fmt.Errorf("%w: %q", ErrUnknownAction, meta.Action)}
	}
	return handler(from, meta)
}

func (vm *machine) Checkpoint() (string, error) { return vm.checkpoint() }
func (vm *machine) Restore(data string) error   { return vm.restore(data) }
func (vm *machine) Close() error                { return vm.close() }
