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

```go
func (v *App) CheckPermission(from string, meta schema.Meta) (schema.Result, bool) {
    if err := v.authorize(from, meta); err != nil {
        return schema.Result{Error: err}, true
    }
    return schema.Result{}, false
}
```

Callbacks run in registration order for every Apply, before looking up the Action:

- `handled=false`: ignore the Result and continue.
- `handled=true`: return the Result unchanged; skip all remaining callbacks and the Action. An empty Result is also a valid early return.
- If every callback allows processing, look up `meta.Action` and execute it. Missing actions return `ErrUnknownAction`.

Multiple registrations append, including repeated callbacks. Nil callbacks are rejected at Build. BeforeApply only supports preprocessing; there is no Context or Next.

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
