# hvmkit

A small Go library for building [hymx](https://github.com/hymatrix/hymx) VMs. Create an app, register callbacks, and build a VM.

Requires Go 1.24+. Uses hymx v0.6.0.

## Install

```sh
go get github.com/hymatrix/hvmkit
```

## Usage

Write a regular Spawn function. Create a fresh business instance for each VM and register its callbacks directly:

```go
func Spawn(env schema.Env) (schema.Vm, error) {
    v, err := NewApp(env)
    if err != nil {
        return nil, err
    }

    app := hvmkit.New()
    app.BeforeApply(v.CheckAvailable, v.SkipRecovery)
    app.BeforeApply(v.CheckPermission)
    app.Action("ssh", v.SSH)
    app.Checkpoint(v.Checkpoint)
    app.Restore(v.Restore)
    app.Close(v.Close)
    return app.Build()
}
```

Here `schema` is `github.com/hymatrix/hymx/vmm/schema`; `NewApp` and its methods belong to your application. No business interface is required.

Mount the Spawn function in your existing hymx startup code:

```go
if err := server.Mount("hifire.0.1.0", Spawn); err != nil {
    return err
}
```

## BeforeApply

Before callbacks use `func(*hvmkit.Context)` with no return value:

```go
func (v *App) CheckPermission(c *hvmkit.Context) {
    if err := v.authorize(c.From, c.Meta); err != nil {
        c.Stop(schema.Result{Error: err})
        return
    }
    // Continue automatically.
}

func (v *App) SkipRecovery(c *hvmkit.Context) {
    if v.shouldSkipRecovery(c.Meta) {
        c.Stop(schema.Result{}) // Empty success; skip subsequent processing.
        return
    }
}
```

Callbacks run in registration order for every Apply, before looking up the Action:

- Returning normally (including a plain `return`) continues to the next callback.
- `c.Stop(result)` marks the request as stopped. After the current callback returns, Apply returns that Result unchanged and skips all remaining callbacks and the Action.
- **Stop does not exit the current function. Usually write `return` immediately after it. A plain `return` without Stop does not stop the chain.** Repeated Stop calls use the last Result.
- If every callback allows processing, look up `c.Meta.Action` and execute it. Missing actions return `ErrUnknownAction`.

Each Apply creates its own Context, shared by that request's before callbacks. `c.From` and `c.Meta` initially contain the Apply arguments; changes are passed to later callbacks and the Action. Only use the Context during the callback, without concurrent access.

Multiple registrations append, including repeated callbacks. Nil callbacks are rejected at Build. This uses Gin-style automatic continuation with explicit stopping, but only supports preprocessing: there is no Next, Abort alias, or postprocessing mechanism. Action keeps its existing `func(from string, meta schema.Meta) schema.Result` signature.

## Stateless VMs

For a VM with no business state, use Stateless instead of Checkpoint and Restore:

```go
func Spawn(env schema.Env) (schema.Vm, error) {
    app := hvmkit.New()
    app.Stateless()
    app.Action("echo", func(from string, meta schema.Meta) schema.Result {
        return schema.Result{Output: meta.Data}
    })
    return app.Build()
}
```

## Build and lifecycle

- Build validates registration and freezes the App on both success and failure. Subsequent registration panics with `ErrAppFrozen`; subsequent Build calls return that error.
- Invalid configuration returns `ErrInvalidConfig`. Action names must be nonblank and unique; callbacks must be non-nil. Lifecycle callbacks can only be registered once.
- Stateful VMs require both Checkpoint and Restore. Stateless conflicts with either callback. Close is optional.
- On Build failure, the first registered Close callback, if non-nil, runs once. Cleanup errors are joined with configuration errors. Register Close even if earlier registrations are invalid.
- On success, the VM owns the callbacks and Close is invoked by its caller. Close is not made idempotent. Constructor failures must clean up their own partial resources.
- App's zero value is usable. Do not copy an App or register concurrently. Business state, persistence, transactions, and callback synchronization belong to your application.
- Replay and dry-run requests pass through the same before chain and Action dispatch. Any skipping policy belongs in your callbacks.

See the [counter example](examples/counter) for a complete implementation.

## Migration

`App` replaces `Builder`, and `ErrAppFrozen` replaces `ErrBuilderFrozen`. `Factory` has been removed: create and register your business instance inside a regular Spawn function. Build now handles cleanup on failure, so do not close the same instance again after a failed Build.

BeforeApply callbacks now take `*hvmkit.Context`. Replace `return result, true` with `c.Stop(result); return`, and replace `return schema.Result{}, false` with a plain `return` or normal function completion.
