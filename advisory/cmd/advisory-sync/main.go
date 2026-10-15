// Package main refreshes the advisory register (NSM RSS) and the CISA
// KEV snapshot. It runs on the host — before an instance launches, or on
// a schedule — never inside a scan instance.
//
// Usage (from the scopeweaver/ repo root):
//
//	go run ./advisory/cmd/advisory-sync
//	go run ./advisory/cmd/advisory-sync -seen 2026-10-15
//	go run ./advisory/cmd/advisory-sync -skip-kev
//
// The default -register and -kev paths are relative to the repo root.
// -skip-nsm / -skip-kev leave one source alone (e.g. when only one feed
// is reachable); the other still syncs.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Autumn-27/artex/advisory"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "advisory-sync:", err)
		os.Exit(1)
	}
}

// run parses the flags, performs one sync, and prints a result line for
// each source that ran. If Sync returned an error it exits non-zero after
// printing what succeeded.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advisory-sync", flag.ContinueOnError)
	fs.SetOutput(stdout)
	register := fs.String("register", advisory.DefaultRegisterPath, "path to the JSONL register file (repo-root relative)")
	kev := fs.String("kev", advisory.DefaultKEVPath, "path to the KEV snapshot file (repo-root relative)")
	nsmURL := fs.String("nsm-url", advisory.DefaultNSMRSSURL, "NSM RSS feed URL")
	kevURL := fs.String("kev-url", advisory.DefaultKEVURL, "CISA KEV catalog URL")
	seenStr := fs.String("seen", time.Now().UTC().Format("2006-01-02"), "first-seen date (YYYY-MM-DD) recorded on new entries")
	skipNSM := fs.Bool("skip-nsm", false, "skip the NSM RSS feed and register update")
	skipKEV := fs.Bool("skip-kev", false, "skip the CISA KEV catalog and snapshot update")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: advisory-sync [-register <file>] [-kev <file>] [-nsm-url <url>] [-kev-url <url>] [-seen YYYY-MM-DD] [-skip-nsm] [-skip-kev]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	seen, err := time.Parse("2006-01-02", *seenStr)
	if err != nil {
		return fmt.Errorf("-seen must be YYYY-MM-DD: %w", err)
	}
	res, syncErr := advisory.Sync(context.Background(), advisory.SyncOptions{
		NSMRSSURL:    *nsmURL,
		KEVURL:       *kevURL,
		RegisterPath: *register,
		KEVPath:      *kev,
		Seen:         seen,
		SkipNSM:      *skipNSM,
		SkipKEV:      *skipKEV,
	})
	if !*skipNSM {
		fmt.Fprintf(stdout, "NSM: added %d (total %d)\n", res.NSMAdded, res.NSMTotal)
	}
	if !*skipKEV {
		status := "unchanged"
		if res.KEVUpdated {
			status = "updated"
		}
		fmt.Fprintf(stdout, "KEV: catalog %s, %d entries, %s\n",
			res.KEVVersion, res.KEVCount, status)
	}
	return syncErr
}
