package hvmkit_test

import (
	"errors"
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
		setup func(*hvmkit.Builder)
		want  string
	}{
		{"missing lifecycle", func(*hvmkit.Builder) {}, "requires both"},
		{"missing restore", func(b *hvmkit.Builder) { b.Checkpoint(func() (string, error) { return "", nil }) }, "requires both"},
		{"missing checkpoint", func(b *hvmkit.Builder) { b.Restore(func(string) error { return nil }) }, "requires both"},
		{"empty action", func(b *hvmkit.Builder) { b.Stateless(); b.Action("", noop) }, "must not be blank"},
		{"blank action", func(b *hvmkit.Builder) { b.Stateless(); b.Action(" \t", noop) }, "must not be blank"},
		{"duplicate action", func(b *hvmkit.Builder) { b.Stateless(); b.Action("A", noop); b.Action("A", noop) }, "duplicate action"},
		{"nil handler", func(b *hvmkit.Builder) { b.Stateless(); b.Action("A", nil) }, "nil handler"},
		{"nil checkpoint", func(b *hvmkit.Builder) { b.Checkpoint(nil); b.Restore(func(string) error { return nil }) }, "nil Checkpoint"},
		{"nil restore", func(b *hvmkit.Builder) { b.Checkpoint(func() (string, error) { return "", nil }); b.Restore(nil) }, "nil Restore"},
		{"nil close", func(b *hvmkit.Builder) { b.Stateless(); b.Close(nil) }, "nil Close"},
		{"duplicate checkpoint", func(b *hvmkit.Builder) { b.Checkpoint(func() (string, error) { return "", nil }); b.Checkpoint(nil) }, "duplicate Checkpoint"},
		{"duplicate restore", func(b *hvmkit.Builder) { b.Restore(func(string) error { return nil }); b.Restore(nil) }, "duplicate Restore"},
		{"duplicate close", func(b *hvmkit.Builder) { b.Stateless(); b.Close(func() error { return nil }); b.Close(nil) }, "duplicate Close"},
		{"stateless checkpoint", func(b *hvmkit.Builder) { b.Stateless(); b.Checkpoint(func() (string, error) { return "", nil }) }, "conflicts"},
		{"restore then stateless", func(b *hvmkit.Builder) { b.Restore(func(string) error { return nil }); b.Stateless() }, "conflicts"},
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

func TestStatelessAndFrozenBuilder(t *testing.T) {
	var b hvmkit.Builder
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
	if _, err := b.Build(); !errors.Is(err, hvmkit.ErrBuilderFrozen) {
		t.Fatalf("second Build: %v", err)
	}
	for _, mutate := range []func(){
		func() { b.Action("A", noop) }, func() { b.Checkpoint(nil) },
		func() { b.Restore(nil) }, func() { b.Close(nil) }, func() { b.Stateless() },
	} {
		func() {
			defer func() {
				if got := recover(); got != hvmkit.ErrBuilderFrozen {
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
