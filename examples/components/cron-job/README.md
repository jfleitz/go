# Cron Job

A scheduled job in Go: a wasmCloud **service** that implements
`wasi:cli/run` and runs a job on a cron schedule for as long as the workload
is up. There is no HTTP handler and nothing calls it; the host starts it, and
it wakes itself up.

Logs go through `wasi:logging` with `go.bytecodealliance.org/pkg/wasilog`, and
the schedule is parsed with [`robfig/cron`](https://github.com/robfig/cron).

## Prerequisites

- Go 1.27+
- [`wash`](https://wasmcloud.com/docs/installation) 2.x

On a fresh clone, download the Go modules first:

```shell
go mod download
```

## Develop

```shell
wash dev
```

The job logs a line on every tick (every 10 seconds in dev):

```text
INFO wasi.logging.log{level=Level::Info context="cron-job"}: schedule="*/10 * * * * *" next="2026-09-30T15:42:50Z" scheduler started
INFO wasi.logging.log{level=Level::Info context="cron-job"}: run="1" scheduled="2026-09-30T15:42:50Z" heap_bytes="39976" job running
INFO wasi.logging.log{level=Level::Info context="cron-job"}: run="1" scheduled="2026-09-30T15:42:50Z" took="1.423292ms" next="2026-09-30T15:43:00Z" job finished
```

Two settings in [.wash/config.yaml](./.wash/config.yaml) make this work:

```yaml
dev:
  service: true          # without this, wash dev never calls wasi:cli/run

workload:
  environment:
    config:
      CRON_SCHEDULE: "*/10 * * * * *"
```

`dev.service: true` is the one that is easy to miss. By default `wash dev`
deploys what it builds as a request-driven *component*, and the host only
calls `wasi:cli/run` on a workload's *service*. Leave it out and the job
builds and deploys cleanly, then never runs, with nothing in the log to say
why.

## How `wasi:cli/run` is wired up

If you have built a command component with TinyGo, this is the part that
changes. Under TinyGo, `main()` *was* the `wasi:cli/run` implementation:
export `wasi:cli/run` from your world, put the work in `main()`, done. That
no longer works, and there are two separate reasons:

1. **`main()` never runs.** componentize-go builds a reactor
   (`go build -buildmode=c-shared`), not a command. The runtime runs every
   `init()`, and after that only the exported functions are ever called.
   `main()` still has to exist, because the compiler requires it, but its
   body is dead code.
2. **`run` is an ordinary export, and it lives in a package you write.**
   Generating bindings for a world that exports `wasi:cli/run` produces this
   glue ([wit_exports/wit_exports.go](./wit_exports/wit_exports.go)):

   ```go
   //go:wasmexport wasi:cli/run@0.2.8#run
   func wasm_export_wasi_cli_run_run() int32 {
       result := export_wasi_cli_run.Run()
       ...
   }
   ```

   `export_wasi_cli_run` is not generated. The glue imports it from
   `<your module>/export_wasi_cli_run` and expects you to supply
   `func Run() witTypes.Result[witTypes.Unit, witTypes.Unit]` there.
   Writing a `Run` in package `main`, or editing the generated file, does
   not hook anything up.

So the wiring takes three pieces:

**The generated glue**, committed under [wit_exports/](./wit_exports), and
blank-imported from `main.go` so it is linked in. Nothing references it by
name, so without the import the component has no `run` export at all:

```go
import _ "github.com/wasmCloud/go/examples/components/cron-job/wit_exports"
```

**A handwritten trampoline**, [export_wasi_cli_run/export.go](./export_wasi_cli_run/export.go).
It holds a slot for the implementation and forwards to it. Keeping it
separate means `wit_exports` can be regenerated at any time without touching
your code:

```go
package export_wasi_cli_run

import witTypes "go.bytecodealliance.org/pkg/wit/types"

var Exports struct {
    Run func() witTypes.Result[witTypes.Unit, witTypes.Unit]
}

func Run() witTypes.Result[witTypes.Unit, witTypes.Unit] {
    return Exports.Run()
}
```

**The implementation**, registered from `init()` in [main.go](./main.go),
which is guaranteed to run before the host calls the export:

```go
func init() {
    export_wasi_cli_run.Exports.Run = func() witTypes.Result[witTypes.Unit, witTypes.Unit] {
        if err := run(); err != nil {
            return witTypes.Err[witTypes.Unit, witTypes.Unit](witTypes.Unit{})
        }
        return witTypes.Ok[witTypes.Unit, witTypes.Unit](witTypes.Unit{})
    }
}

func main() {} // required by the compiler; never runs
```

If you don't want the indirection, you can put the body straight into
`export_wasi_cli_run.Run`. The `Exports` slot is there so that the package
the generator names stays a thin seam, and your code stays in `main`.

## The scheduler

`run` in [main.go](./main.go) parses `CRON_SCHEDULE`, then loops forever:
sleep until the next time the expression fires, call `runJob`, repeat. Put
your own work in `runJob` in [job.go](./job.go).

- **`Run` must not return.** The host calls `wasi:cli/run` once, when the
  service starts. When it returns, the service is finished, whether it
  returned `Ok` or `Err`. The loop only exits if the schedule itself is
  unusable.
- **A failing job does not stop the schedule.** An error from `runJob` is
  logged and the loop moves on to the next tick. A panic is recovered and
  treated the same way, because one that escapes traps the whole service.
- **Runs never overlap.** The next run is scheduled only after the current
  one returns, so a job that overruns its interval skips the ticks it missed.
- **Schedules are in UTC.** They accept standard five-field expressions, an
  optional leading seconds field, and descriptors such as `@hourly` or
  `@every 90s`. With no `CRON_SCHEDULE` set, the job runs at the top of every
  minute.

Plain `time.Sleep` is fine here: this is a synchronous WASI P2 export, and the
Go runtime blocks on the host's clock while it waits.

## Build

```shell
wash build
```

`build.command` passes `-w 'wasmcloud:examples/cron-job@0.1.0'`. That pins
the component to its own [world](./wit/world.wit) (`wasi:cli/imports`,
`wasi:logging`, and the `wasi:cli/run` export) and stops componentize-go
merging in the default world of `go.bytecodealliance.org/pkg`, which exports
`wasi:http/incoming-handler`.

## Regenerating the bindings

`componentize-go build` does not generate bindings; it compiles what is in the
module. After changing the world, or bumping componentize-go, regenerate the
export glue and keep only `wit_exports/` (the import packages it also writes
are already provided by `go.bytecodealliance.org/pkg`):

```shell
wash wit fetch
go tool componentize-go --ignore-toml-files \
  -w 'wasmcloud:examples/cron-job@0.1.0' -d wit \
  bindings --format -o /tmp/cron-job-bindings \
  --pkg-name github.com/wasmCloud/go/examples/components/cron-job
rm -rf wit_exports && cp -r /tmp/cron-job-bindings/wit_exports .
```

`export_wasi_cli_run/` is handwritten and is never touched by this.

## Deploy to wasmCloud on Kubernetes

Push the component to an OCI registry, point `image` in
[deploy/deployment.yaml](./deploy/deployment.yaml) at it, and apply the
manifest:

```shell
wash oci push ghcr.io/<your-org>/cron-job:0.1.0 build/cron_job.wasm
kubectl apply -f deploy/deployment.yaml
```

What matters in the manifest:

- **`service:`, not `components:`.** This is the cluster equivalent of
  `dev.service: true`. The host calls `wasi:cli/run` on the workload's
  service; the same image under `components` deploys fine and never runs.
- **`CRON_SCHEDULE`** is set under `service.localResources.environment`, and
  reaches the job through `wasi:cli/environment` (`os.Getenv`). To keep it
  out of the manifest, use `configFrom` to point at a ConfigMap instead.
- **`replicas: 1`.** Every replica runs its own copy of the schedule, so
  two replicas run the job twice per tick. Scale up only if the job is safe
  to run concurrently, or takes a lock first.
- **`wasi:logging`** stays listed under `hostInterfaces`. A real host binds
  only the interfaces the manifest names; without it the workload fails to
  start with `a matching implementation was not found in the linker`.
  `wash dev` derives it from the imports, so dev never shows it. Anything
  you add to the world for your job (`wasi:http`, `wasi:keyvalue`,
  `wasmcloud:nats`, ...) needs an entry here too.

See the wasmCloud [workload deployment
quickstart](https://wasmcloud.com/docs/quickstart/deploy-a-webassembly-workload/)
for cluster setup.
