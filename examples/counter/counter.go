// Package counter demonstrates explicit registration with a per-process factory.
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
var Spawn vmmSchema.VmSpawnFunc = hvmkit.Factory(
	NewApp,
	func(vm *hvmkit.Builder, app *App) {
		vm.Action("Submit", app.Submit)
		vm.Checkpoint(app.Checkpoint)
		vm.Restore(app.Restore)
		// Register vm.Close(app.Close) if the app owns resources.
	},
)
