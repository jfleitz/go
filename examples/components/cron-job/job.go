package main

import (
	"log/slog"
	"runtime"
)

// runJob is the work done on every tick. Replace the body with your own:
// calling an API over wasi:http, sweeping a keyvalue bucket, publishing to
// NATS. Anything it imports has to be added to wit/world.wit and, on a
// cluster, to hostInterfaces in the deployment manifest.
//
// Runs are sequential: the next one is not scheduled until this returns, so a
// job that overruns its interval skips the ticks it missed instead of
// overlapping with itself.
func runJob(log *slog.Logger) error {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	log.Info("job running", "heap_bytes", mem.HeapAlloc)
	return nil
}
