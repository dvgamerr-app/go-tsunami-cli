package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	// maxProcs caps parallelism: a transformation runs on one goroutine, so
	// more processors only add garbage collector coordination overhead.
	maxProcs = 4
	// gcPercent trades a few extra megabytes of heap for far fewer
	// collections, since streaming jobs keep almost nothing alive.
	gcPercent = 400
	// defaultMemLimit bounds heap growth when collections are delayed, for
	// example on a busy machine.
	defaultMemLimit = 512 << 20
)

// tuneRuntime applies streaming-friendly runtime defaults unless the user
// configured them through GOMAXPROCS, GOGC or GOMEMLIMIT.
func tuneRuntime(memLimit string) error {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(min(runtime.NumCPU(), maxProcs))
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(gcPercent)
	}
	switch {
	case memLimit != "":
		limit, err := parseSize(memLimit)
		if err != nil {
			return err
		}
		if limit == 0 {
			limit = math.MaxInt64
		}
		debug.SetMemoryLimit(limit)
	case os.Getenv("GOMEMLIMIT") == "":
		debug.SetMemoryLimit(defaultMemLimit)
	}
	return nil
}

// parseSize parses sizes such as 512MiB, 2GB or 1048576.
func parseSize(s string) (int64, error) {
	units := []struct {
		suffix string
		mult   int64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
		{"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"B", 1},
	}
	upper := strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range units {
		if strings.HasSuffix(upper, u.suffix) {
			upper, mult = strings.TrimSpace(strings.TrimSuffix(upper, u.suffix)), u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(upper, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(n * float64(mult)), nil
}
