package testflags

import "flag"

func init() {
	if flag.CommandLine.Lookup("no-cache") == nil {
		flag.Bool("no-cache", false, "compatibility flag for external test runners; use -count=1 to disable Go test caching")
	}
}
