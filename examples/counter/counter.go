// Package counter demonstrates explicit registration in a per-process Spawn function.
package counter

import (
	"strconv"

	"github.com/hymatrix/hvmkit"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

type App struct{ count int }

func NewApp(env vmmSchema.Env) (*App, error) { return &App{}, nil }

func (a *App) Submit(from string, meta vmmSchema.Meta) vmmSchema.Result {
	a.count++
	return vmmSchema.Result{Output: a.count}
}

func (a *App) Checkpoint() (string, error) { return strconv.Itoa(a.count), nil }

func (a *App) Restore(data string) error {
	count, err := strconv.Atoi(data)
	if err != nil {
		return err
	}
	a.count = count
	return nil
}

// Spawn can be passed directly to server.Mount("counter.0.1.0", Spawn).
func Spawn(env vmmSchema.Env) (vmmSchema.Vm, error) {
	v, err := NewApp(env)
	if err != nil {
		return nil, err
	}
	app := hvmkit.New()
	app.Action("Submit", v.Submit)
	app.Checkpoint(v.Checkpoint)
	app.Restore(v.Restore)
	// Register app.Close(v.Close) if the business instance owns resources.
	return app.Build()
}
