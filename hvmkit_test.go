package hvmkit_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hymatrix/hvmkit"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func noop(string, vmmSchema.Meta) vmmSchema.Result { return vmmSchema.Result{} }

func TestBuildValidation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*hvmkit.App)
		want  string
	}{
		{"missing lifecycle", func(*hvmkit.App) {}, "requires both"},
		{"missing restore", func(b *hvmkit.App) { b.Checkpoint(func() (string, error) { return "", nil }) }, "requires both"},
		{"missing checkpoint", func(b *hvmkit.App) { b.Restore(func(string) error { return nil }) }, "requires both"},
		{"empty action", func(b *hvmkit.App) { b.Stateless(); b.Action("", noop) }, "must not be blank"},
		{"blank action", func(b *hvmkit.App) { b.Stateless(); b.Action(" \t", noop) }, "must not be blank"},
		{"duplicate action", func(b *hvmkit.App) { b.Stateless(); b.Action("A", noop); b.Action("A", noop) }, "duplicate action"},
		{"nil before", func(b *hvmkit.App) { b.Stateless(); b.BeforeApply(nil) }, "nil BeforeApply"},
		{"nil handler", func(b *hvmkit.App) { b.Stateless(); b.Action("A", nil) }, "nil handler"},
		{"nil checkpoint", func(b *hvmkit.App) { b.Checkpoint(nil); b.Restore(func(string) error { return nil }) }, "nil Checkpoint"},
		{"nil restore", func(b *hvmkit.App) { b.Checkpoint(func() (string, error) { return "", nil }); b.Restore(nil) }, "nil Restore"},
		{"nil close", func(b *hvmkit.App) { b.Stateless(); b.Close(nil) }, "nil Close"},
		{"duplicate checkpoint", func(b *hvmkit.App) { b.Checkpoint(func() (string, error) { return "", nil }); b.Checkpoint(nil) }, "duplicate Checkpoint"},
		{"duplicate restore", func(b *hvmkit.App) { b.Restore(func(string) error { return nil }); b.Restore(nil) }, "duplicate Restore"},
		{"duplicate close", func(b *hvmkit.App) { b.Stateless(); b.Close(func() error { return nil }); b.Close(nil) }, "duplicate Close"},
		{"stateless checkpoint", func(b *hvmkit.App) { b.Stateless(); b.Checkpoint(func() (string, error) { return "", nil }) }, "conflicts"},
		{"restore then stateless", func(b *hvmkit.App) { b.Restore(func(string) error { return nil }); b.Stateless() }, "conflicts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := hvmkit.New()
			tt.setup(b)
			vm, err := b.Build()
			if vm != nil || !errors.Is(err, hvmkit.ErrInvalidConfig) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Build() = %v, %v; want invalid config containing %q", vm, err, tt.want)
			}
		})
	}
}

func TestApplyPreservesInputAndResult(t *testing.T) {
	wantErr := errors.New("business failure")
	want := vmmSchema.Result{
		Messages:    []*vmmSchema.ResMessage{{Target: "refund", Data: "compensation"}},
		Spawns:      []*vmmSchema.ResSpawn{{Data: "child"}},
		Assignments: []interface{}{"assignment"},
		Output:      map[string]int{"count": 1}, Data: "data",
		Cache: map[string]string{"key": "value"}, Error: wantErr,
	}
	b := hvmkit.New()
	b.Stateless()
	var received vmmSchema.Meta
	b.Action("Submit", func(from string, meta vmmSchema.Meta) vmmSchema.Result {
		if from != "sender" {
			t.Fatalf("from = %q", from)
		}
		received = meta
		return want
	})
	vm, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []vmmSchema.ExecMode{vmmSchema.ExecModeApply, vmmSchema.ExecModeReplay, vmmSchema.ExecModeDryRun} {
		meta := vmmSchema.Meta{Action: "Submit", Mode: mode, Timestamp: 123, Params: map[string]string{"x": "y"}, Data: "input"}
		got := vm.Apply("sender", meta)
		if !reflect.DeepEqual(received, meta) || !reflect.DeepEqual(got, want) || got.Messages[0] != want.Messages[0] {
			t.Fatalf("input or result changed in mode %s: %+v", mode, got)
		}
	}
	for _, name := range []string{"", "Missing", "submit"} {
		if got := vm.Apply("", vmmSchema.Meta{Action: name}); !errors.Is(got.Error, hvmkit.ErrUnknownAction) {
			t.Fatalf("unknown action %q: %v", name, got.Error)
		}
	}
}

func TestStatelessAndFrozenApp(t *testing.T) {
	var b hvmkit.App
	b.Stateless()
	b.Stateless()
	vm, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if data, err := vm.Checkpoint(); data != "" || err != nil {
		t.Fatalf("Checkpoint = %q, %v", data, err)
	}
	if err := vm.Restore("ignored"); err != nil {
		t.Fatal(err)
	}
	if err := vm.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(); !errors.Is(err, hvmkit.ErrAppFrozen) {
		t.Fatalf("second Build: %v", err)
	}
	for _, mutate := range []func(){
		func() { b.BeforeApply(nil) }, func() { b.Action("A", noop) }, func() { b.Checkpoint(nil) },
		func() { b.Restore(nil) }, func() { b.Close(nil) }, func() { b.Stateless() },
	} {
		func() {
			defer func() {
				if got := recover(); got != hvmkit.ErrAppFrozen {
					t.Fatalf("panic = %v", got)
				}
			}()
			mutate()
		}()
	}
	if got := vm.Apply("", vmmSchema.Meta{Action: "A"}); !errors.Is(got.Error, hvmkit.ErrUnknownAction) {
		t.Fatal("frozen VM changed")
	}
}

func TestLifecyclePassthrough(t *testing.T) {
	wantErr := errors.New("storage failure")
	b := hvmkit.New()
	b.Checkpoint(func() (string, error) { return "partial", wantErr })
	b.Restore(func(data string) error {
		if data != "snapshot" {
			t.Fatal(data)
		}
		return wantErr
	})
	b.Close(func() error { return wantErr })
	vm, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if data, err := vm.Checkpoint(); data != "partial" || err != wantErr {
		t.Fatalf("checkpoint: %q %v", data, err)
	}
	if err := vm.Restore("snapshot"); err != wantErr {
		t.Fatal(err)
	}
	if err := vm.Close(); err != wantErr {
		t.Fatal(err)
	}
}

func TestBeforeApply(t *testing.T) {
	wantErr := errors.New("denied")
	for _, stop := range []int{0, 1, 2, 3} {
		for _, action := range []string{"A", "missing"} {
			t.Run(fmt.Sprintf("stop=%d/action=%s", stop, action), func(t *testing.T) {
				app := hvmkit.New()
				app.Stateless()
				var calls []int
				meta := vmmSchema.Meta{Action: action, Data: "input"}
				want := vmmSchema.Result{Output: "stopped", Error: wantErr, Messages: []*vmmSchema.ResMessage{{Target: "refund"}}}
				before := func(n int) hvmkit.BeforeApplyHandler {
					return func(from string, got vmmSchema.Meta) (vmmSchema.Result, bool) {
						if from != "sender" || !reflect.DeepEqual(got, meta) {
							t.Fatalf("changed input: %q %+v", from, got)
						}
						calls = append(calls, n)
						return want, n == stop
					}
				}
				app.BeforeApply(before(1), before(2))
				app.BeforeApply(before(3))
				app.Action("A", func(string, vmmSchema.Meta) vmmSchema.Result {
					calls = append(calls, 4)
					return vmmSchema.Result{Output: "action"}
				})
				vm, err := app.Build()
				if err != nil {
					t.Fatal(err)
				}
				got := vm.Apply("sender", meta)
				wantCalls := []int{1, 2, 3}
				if stop != 0 {
					wantCalls = wantCalls[:stop]
					if !reflect.DeepEqual(got, want) || got.Messages[0] != want.Messages[0] {
						t.Fatalf("changed result: %+v", got)
					}
				} else if action == "A" {
					wantCalls = append(wantCalls, 4)
					if got.Output != "action" || got.Error != nil {
						t.Fatalf("result: %+v", got)
					}
				} else if !errors.Is(got.Error, hvmkit.ErrUnknownAction) {
					t.Fatalf("unknown action: %+v", got)
				}
				if !reflect.DeepEqual(calls, wantCalls) {
					t.Fatalf("calls = %v, want %v", calls, wantCalls)
				}
			})
		}
	}
}

func TestBeforeApplyRepeatedAndEmptySuccess(t *testing.T) {
	app := hvmkit.New()
	app.Stateless()
	calls := 0
	before := func(string, vmmSchema.Meta) (vmmSchema.Result, bool) {
		calls++
		return vmmSchema.Result{}, calls == 2
	}
	app.BeforeApply()
	app.BeforeApply(before, before)
	app.BeforeApply(func(string, vmmSchema.Meta) (vmmSchema.Result, bool) {
		t.Fatal("called after handled=true")
		return vmmSchema.Result{}, false
	})
	vm, err := app.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got := vm.Apply("", vmmSchema.Meta{Action: "missing"}); !reflect.DeepEqual(got, vmmSchema.Result{}) || calls != 2 {
		t.Fatalf("result = %+v, calls = %d", got, calls)
	}
}

func TestBuildFailureCleanup(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	for _, wantErr := range []error{nil, cleanupErr} {
		app := hvmkit.New()
		closed := 0
		app.Close(func() error { closed++; return wantErr })
		// The first Close must remain registered even if a duplicate is supplied.
		app.Close(func() error { t.Fatal("duplicate Close called"); return nil })
		vm, err := app.Build()
		if vm != nil || !errors.Is(err, hvmkit.ErrInvalidConfig) || closed != 1 {
			t.Fatalf("Build = %v, %v; closed = %d", vm, err, closed)
		}
		if wantErr != nil && !errors.Is(err, wantErr) {
			t.Fatalf("lost cleanup error: %v", err)
		}
		if _, err := app.Build(); !errors.Is(err, hvmkit.ErrAppFrozen) || closed != 1 {
			t.Fatalf("second Build = %v; closed = %d", err, closed)
		}
		func() {
			defer func() {
				if got := recover(); got != hvmkit.ErrAppFrozen {
					t.Fatalf("panic = %v", got)
				}
			}()
			app.BeforeApply(nil)
		}()
	}
}
