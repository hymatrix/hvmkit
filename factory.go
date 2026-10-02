package hvmkit

import (
	"errors"
	"fmt"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

// Factory produces a native hymx Spawn function without constructing an app.
// Each Spawn calls newApp and register with a fresh Builder, then builds a VM.
// T has no interface constraints; all callbacks must be registered explicitly.
//
// If construction fails, newApp is responsible for partial resource cleanup.
// If Build fails, Factory calls the first registered Close callback if non-nil
// and joins any cleanup error with the Build error. It does not discover
// methods on T or recover panics. A successful constructor must return a usable
// app; Factory does not inspect T for nil values or shared state.
// The registration callback must not call Build itself.
func Factory[T any](
	newApp func(vmmSchema.Env) (T, error),
	register func(*Builder, T),
) vmmSchema.VmSpawnFunc {
	return func(env vmmSchema.Env) (vmmSchema.Vm, error) {
		if newApp == nil || register == nil {
			return nil, fmt.Errorf("%w: Factory requires constructor and registration callbacks", ErrInvalidConfig)
		}
		app, err := newApp(env)
		if err != nil {
			return nil, fmt.Errorf("hvmkit: construct app: %w", err)
		}
		builder := New()
		register(builder, app)
		vm, err := builder.Build()
		if err != nil && builder.close != nil {
			if closeErr := builder.close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("hvmkit: cleanup after build failure: %w", closeErr))
			}
		}
		return vm, err
	}
}
