// Command leasekeeper is a local lease broker for developers who run several
// coding agents against one repository, GPU or API quota.
//
// It gives agents a two-verb ownership API — acquire and release — over named
// resources (file, port, gpu, quota) and refuses a circular wait before it
// deadlocks the fleet.
package main

import (
	"context"
	"os"

	"github.com/SuperMarioYL/leasekeeper/internal/cli"
)

func main() {
	// The wrapped agent shares this process group, so Ctrl-C reaches it
	// directly. Lease cleanup does not depend on catching the signal: the
	// broker counts live sessions, so a run that dies is noticed as a dropped
	// connection and its leases are handed back.
	os.Exit(cli.Execute(context.Background()))
}
