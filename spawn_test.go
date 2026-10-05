package hvmkit_test

import (
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

func TestSpawnIsolationAndReplay(t *testing.T) {
	var apps []*counter
	spawn := func(env vmmSchema.Env) (vmmSchema.Vm, error) {
		v := &counter{}
		apps = append(apps, v)
		app := hvmkit.New()
		app.Action("Submit", v.submit)
		app.Checkpoint(v.checkpoint)
		app.Restore(v.restore)
		app.Close(v.close)
		return app.Build()
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
	if len(apps) != 3 {
		t.Fatal("expected one business instance per Spawn")
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
