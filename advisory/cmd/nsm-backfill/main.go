// Package main backfills the advisory register from saved NSM archive
// pages. It is a one-time dev tool (roadmap E.7d): parse every page,
// de-duplicate by record ID, merge against the existing register file,
// and append the new entries.
//
// Usage:
//
//	go run ./advisory/cmd/nsm-backfill -register advisory/register/nsm.jsonl -seen 2026-10-15 <page.html>...
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Autumn-27/artex/advisory"
)

func main() {
	registerPath := flag.String("register", "advisory/register/nsm.jsonl", "path to the JSONL register file")
	seenStr := flag.String("seen", "", "first-seen date (YYYY-MM-DD) recorded on new entries")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: nsm-backfill -register <file> -seen YYYY-MM-DD <page.html>...")
		flag.PrintDefaults()
	}
	flag.Parse()
	pages := flag.Args()
	if len(pages) == 0 || *seenStr == "" {
		flag.Usage()
		os.Exit(1)
	}
	seen, err := time.Parse("2006-01-02", *seenStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nsm-backfill: -seen must be YYYY-MM-DD: %v\n", err)
		os.Exit(1)
	}
	added, present, total, err := run(*registerPath, seen, pages)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nsm-backfill:", err)
		os.Exit(1)
	}
	fmt.Printf("added %d, already present %d, total %d\n", added, present, total)
}

// run parses the archive pages, de-duplicating records by ID across
// pages, merges them against the register file (a missing file counts as
// empty), appends the new entries, and reports how many were added, how
// many were already present, and the new total.
func run(registerPath string, seen time.Time, pages []string) (added, present, total int, err error) {
	recs := make([]advisory.Record, 0, len(pages)*10)
	have := map[string]bool{}
	for _, p := range pages {
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return 0, 0, 0, fmt.Errorf("read %s: %w", p, rerr)
		}
		pageRecs, perr := advisory.ParseNSMArchivePage(b)
		if perr != nil {
			return 0, 0, 0, fmt.Errorf("parse %s: %w", p, perr)
		}
		for _, r := range pageRecs {
			if !have[r.ID] {
				have[r.ID] = true
				recs = append(recs, r)
			}
		}
	}
	var existing []advisory.RegisterEntry
	raw, oerr := os.ReadFile(registerPath)
	if oerr == nil {
		existing, err = advisory.ReadRegister(bytes.NewReader(raw))
		if err != nil {
			return 0, 0, 0, fmt.Errorf("read register %s: %w", registerPath, err)
		}
	} else if !os.IsNotExist(oerr) {
		return 0, 0, 0, fmt.Errorf("read register %s: %w", registerPath, oerr)
	}
	merged := advisory.MergeRegister(existing, recs, seen)
	if len(merged) > 0 {
		f, aerr := os.OpenFile(registerPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if aerr != nil {
			return 0, 0, 0, fmt.Errorf("open register %s for append: %w", registerPath, aerr)
		}
		defer f.Close()
		if werr := advisory.AppendRegister(f, merged); werr != nil {
			return 0, 0, 0, fmt.Errorf("append register %s: %w", registerPath, werr)
		}
	}
	return len(merged), len(recs) - len(merged), len(existing) + len(merged), nil
}
