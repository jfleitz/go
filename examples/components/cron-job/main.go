// A scheduled job on wasmCloud, in Go: a cron trigger that runs as a
// long-lived service.
//
// The host calls this component's `wasi:cli/run` export once, when the
// service starts. Run then loops for the life of the workload, sleeping until
// the next time the cron expression fires and calling runJob each time.
//
// Two things differ from a TinyGo command component, where main() *was* the
// run export:
//
//   - componentize-go builds a reactor (`-buildmode=c-shared`), so main()
//     never runs. Only init() and the exports do.
//   - `wasi:cli/run#run` is an ordinary export. The generated glue in
//     wit_exports calls export_wasi_cli_run.Run, and init() below plugs the
//     scheduler into it.
//
// Build:  wash build        (see .wash/config.yaml)
// Run:    wash dev          (dev.service: true is what makes it start)
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/robfig/cron/v3"
	"go.bytecodealliance.org/pkg/wasilog"
	witTypes "go.bytecodealliance.org/pkg/wit/types"

	"github.com/wasmCloud/go/examples/components/cron-job/export_wasi_cli_run"

	// Links the generated `//go:wasmexport` glue for `wasi:cli/run` into the
	// component. Nothing references it by name, so the blank import is what
	// keeps it; without it the component has no run export at all.
	_ "github.com/wasmCloud/go/examples/components/cron-job/wit_exports"
)

// The schedule used when CRON_SCHEDULE is not set: the top of every minute.
const defaultSchedule = "0 * * * * *"

// Accepts standard five-field expressions, an optional leading seconds field,
// and descriptors such as @hourly or @every 90s.
var parser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

func init() {
	export_wasi_cli_run.Exports.Run = func() witTypes.Result[witTypes.Unit, witTypes.Unit] {
		if err := run(); err != nil {
			wasilog.ContextLogger("cron-job").Error("scheduler stopped", "error", err)
			return witTypes.Err[witTypes.Unit, witTypes.Unit](witTypes.Unit{})
		}
		return witTypes.Ok[witTypes.Unit, witTypes.Unit](witTypes.Unit{})
	}
}

// run is the scheduler. It returns only if the schedule cannot be used;
// returning ends the service, so a failing job must not make it return.
func run() error {
	log := wasilog.ContextLogger("cron-job")

	expr := os.Getenv("CRON_SCHEDULE")
	if expr == "" {
		expr = defaultSchedule
	}
	schedule, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid CRON_SCHEDULE %q: %w", expr, err)
	}

	now := time.Now().UTC()
	log.Info("scheduler started", "schedule", expr, "next", schedule.Next(now).Format(time.RFC3339))

	for run := 1; ; run++ {
		next := schedule.Next(time.Now().UTC())
		if next.IsZero() {
			// A valid expression that can never fire again, such as a date
			// that does not exist. There is nothing left to wait for.
			return fmt.Errorf("schedule %q has no future run times", expr)
		}
		time.Sleep(time.Until(next))

		started := time.Now()
		jobLog := log.With("run", run, "scheduled", next.Format(time.RFC3339))
		if err := safeRunJob(jobLog); err != nil {
			jobLog.Error("job failed", "error", err, "took", time.Since(started).String())
			continue
		}
		jobLog.Info("job finished", "took", time.Since(started).String(), "next", schedule.Next(time.Now().UTC()).Format(time.RFC3339))
	}
}

// safeRunJob turns a panic in the job into an error. A panic that escapes
// traps the whole service, and one bad run should not stop the schedule.
func safeRunJob(log *slog.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job panicked: %v", r)
		}
	}()
	return runJob(log)
}

// The service is driven through its `wasi:cli/run` export; main never runs.
func main() {}
