package hvmkit_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/hymatrix/hvmkit"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

type counter struct {
	count  int
	closed int
}

func (a *counter) submit(_ string, _ vmmSchema.Meta) vmmSchema.Result {
	a.count++
	return vmmSchema.Result{Output: a.count}
}
func (a *counter) checkpoint() (string, error) { return strconv.Itoa(a.count), nil }
func (a *counter) restore(data string) error {
	n, err := strconv.Atoi(data)
	if err == nil {
		a.count = n
	}
	return err
}
func (a *counter) close() error { a.closed++; return nil }

func registerCounter(b *hvmkit.Builder, a *counter) {
	b.Action("Submit", a.submit)
	b.Checkpoint(a.checkpoint)
	b.Restore(a.restore)
	b.Close(a.close)
}

func TestFactoryIsolationAndReplay(t *testing.T) {
	var apps []*counter
	var received []vmmSchema.Env
	spawn := hvmkit.Factory(func(env vmmSchema.Env) (*counter, error) {
		received = append(received, env)
		app := &counter{}
		apps = append(apps, app)
		return app, nil
	}, registerCounter)
	if len(apps) != 0 {
		t.Fatal("Factory constructed app eagerly")
	}
	newVM := func(nonce int64) vmmSchema.Vm {
		t.Helper()
		vm, err := spawn(vmmSchema.Env{Nonce: nonce})
		if err != nil {
			t.Fatal(err)
		}
		return vm
	}
	original, restored, replayed := newVM(1), newVM(2), newVM(3)
	if len(apps) != 3 || received[1].Nonce != 2 {
		t.Fatal("constructor/env not forwarded per Spawn")
	}
	apply := func(vm vmmSchema.Vm, mode vmmSchema.ExecMode) int {
		return vm.Apply("sender", vmmSchema.Meta{Action: "Submit", Mode: mode}).Output.(int)
	}
	apply(original, vmmSchema.ExecModeApply)
	snapshot, err := original.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	want := apply(original, vmmSchema.ExecModeApply)
	if apps[1].count != 0 || apps[2].count != 0 {
		t.Fatal("Spawn instances share state")
	}
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if got := apply(restored, vmmSchema.ExecModeReplay); got != want {
		t.Fatalf("restored output = %d, want %d", got, want)
	}
	apply(replayed, vmmSchema.ExecModeReplay)
	if got := apply(replayed, vmmSchema.ExecModeReplay); got != want {
		t.Fatal("full replay differs")
	}
	for _, vm := range []vmmSchema.Vm{original, restored, replayed} {
		data, err := vm.Checkpoint()
		if data != "2" || err != nil {
			t.Fatalf("state = %q, %v", data, err)
		}
		if err := vm.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, app := range apps {
		if app.closed != 1 {
			t.Fatalf("Close calls = %d", app.closed)
		}
	}
}

func TestFactoryFailuresAndCleanup(t *testing.T) {
	constructErr, cleanupErr := errors.New("construct failed"), errors.New("cleanup failed")
	registered := false
	spawn := hvmkit.Factory(func(vmmSchema.Env) (*counter, error) { return nil, constructErr },
		func(*hvmkit.Builder, *counter) { registered = true })
	if vm, err := spawn(vmmSchema.Env{}); vm != nil || !errors.Is(err, constructErr) || registered {
		t.Fatalf("constructor failure: %v %v", vm, err)
	}

	for _, cleanupFailure := range []bool{false, true} {
		closed := 0
		spawn := hvmkit.Factory(func(vmmSchema.Env) (*counter, error) { return &counter{}, nil },
			func(b *hvmkit.Builder, a *counter) {
				b.Action("invalid", nil)
				b.Close(func() error {
					closed++
					if cleanupFailure {
						return cleanupErr
					}
					return nil
				})
			})
		vm, err := spawn(vmmSchema.Env{})
		if vm != nil || !errors.Is(err, hvmkit.ErrInvalidConfig) || closed != 1 || errors.Is(err, cleanupErr) != cleanupFailure {
			t.Fatalf("build failure: %v %v, closed %d", vm, err, closed)
		}
	}
	spawn = hvmkit.Factory(func(vmmSchema.Env) (*counter, error) { return &counter{}, nil },
		func(b *hvmkit.Builder, a *counter) { b.Action("A", nil) })
	if vm, err := spawn(vmmSchema.Env{}); vm != nil || !errors.Is(err, hvmkit.ErrInvalidConfig) {
		t.Fatalf("no cleanup: %v %v", vm, err)
	}
}

func TestFactoryNilCallbacks(t *testing.T) {
	calls := 0
	constructor := func(vmmSchema.Env) (*counter, error) { calls++; return &counter{}, nil }
	for _, spawn := range []vmmSchema.VmSpawnFunc{
		hvmkit.Factory[*counter](nil, registerCounter),
		hvmkit.Factory(constructor, nil),
	} {
		if vm, err := spawn(vmmSchema.Env{}); vm != nil || !errors.Is(err, hvmkit.ErrInvalidConfig) {
			t.Fatalf("nil callback: %v %v", vm, err)
		}
	}
	if calls != 0 {
		t.Fatal("constructed app with nil registration callback")
	}
}
