# hvmkit

A small Go library for building [hymx](https://github.com/hymatrix/hymx) VMs. Register your actions and lifecycle callbacks, then let a factory create a VM for each process.

Requires Go 1.24+. Uses hymx v0.5.0.

## Install

```sh
go get github.com/hymatrix/hvmkit
```

## Usage

Define your app and register its methods explicitly:

```go
package counter

import (
    "strconv"

    "github.com/hymatrix/hvmkit"
    vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

type App struct {
    count int
}

func NewApp(env vmmSchema.Env) (*App, error) {
    return &App{}, nil
}

func (a *App) Submit(from string, meta vmmSchema.Meta) vmmSchema.Result {
    a.count++
    return vmmSchema.Result{Output: a.count}
}

func (a *App) Checkpoint() (string, error) {
    return strconv.Itoa(a.count), nil
}

func (a *App) Restore(data string) error {
    count, err := strconv.Atoi(data)
    if err != nil {
        return err
    }
    a.count = count
    return nil
}

var Spawn = hvmkit.Factory(
    NewApp,
    func(vm *hvmkit.Builder, app *App) {
        vm.Action("Submit", app.Submit)
        vm.Checkpoint(app.Checkpoint)
        vm.Restore(app.Restore)
        // Optional resource cleanup: vm.Close(app.Close)
    },
)
```

Mount it in your existing hymx startup code:

```go
if err := server.Mount("counter.0.1.0", Spawn); err != nil {
    return err
}
```

Each Spawn calls `NewApp`, registers callbacks, and builds a VM. Actions dispatch by `meta.Action` and return your `Result` unchanged. Your app does not need to implement a hvmkit interface.

## Stateless VMs

For a VM without business state, use `Stateless()` instead of registering Checkpoint and Restore:

```go
func Spawn(env vmmSchema.Env) (vmmSchema.Vm, error) {
    vm := hvmkit.New()
    vm.Stateless()
    vm.Action("Echo", func(from string, meta vmmSchema.Meta) vmmSchema.Result {
        return vmmSchema.Result{Output: meta.Data}
    })
    return vm.Build()
}
```

## Notes

- Stateful VMs require both Checkpoint and Restore. They must save and restore all business state needed for later execution.
- Close is optional. If factory construction succeeds but VM building fails, the factory calls the registered Close callback to release resources.
- Build rejects invalid or duplicate registrations and freezes configuration on success.
- Your app owns storage, transactions, and state isolation. Replay and dry-run actions still execute normally.

See the [counter example](examples/counter) for a complete implementation.
