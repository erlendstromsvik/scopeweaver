package advisory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Autumn-27/artex/locale"
)

// Default endpoints and register/snapshot paths for the host-side sync.
// The paths are relative to the repo root; run the sync command from there.
const (
	DefaultNSMRSSURL    = "https://nsm.no/rss/sarbarhetsvarsler-fra-nsm/"
	DefaultKEVURL       = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	DefaultRegisterPath = "advisory/register/nsm.jsonl"
	DefaultKEVPath      = "standards/sources/cisa-kev/known_exploited_vulnerabilities.json"
)

// Body-size caps for the two feeds, generous but bounded so a hostile or
// broken endpoint cannot exhaust memory during a sync.
const (
	nsmRSSMaxBytes = 5 * 1024 * 1024
	kevMaxBytes    = 50 * 1024 * 1024
)

// SyncOptions configures one host-side advisory sync. Zero values select
// the defaults; Client nil means a fresh client with a 30s timeout.
type SyncOptions struct {
	NSMRSSURL    string       // NSM vulnerability-alert RSS feed
	KEVURL       string       // CISA KEV catalog JSON
	RegisterPath string       // append-only NSM register (JSONL)
	KEVPath      string       // vendored KEV snapshot file
	Seen         time.Time    // first-seen date recorded on new register entries
	Client       *http.Client // nil -> a client with a 30s timeout
	SkipNSM      bool         // do not fetch or update the NSM register
	SkipKEV      bool         // do not fetch or update the KEV snapshot
}

// SyncResult reports what the sync changed.
type SyncResult struct {
	NSMAdded   int    // register entries appended this run
	NSMTotal   int    // register entries after the append
	KEVVersion string // catalogVersion of the snapshot now on disk
	KEVCount   int    // KEV entries in that snapshot
	KEVUpdated bool   // the snapshot file was rewritten
}

// Sync refreshes the advisory register from the NSM RSS feed and the CISA
// KEV snapshot from the CISA feed. It runs on the host before an instance
// launches (or on a schedule); scan instances never fetch. The two
// sources are independent: one failing (network, bad payload) never
// prevents the other from updating, and each leaves its own file untouched
// on its own failure. Errors from both sources are joined and returned
// together with the partial result. The KEV file is only replaced after
// the new bytes parse cleanly, and only when the catalog version actually
// changed, so a bad feed can never clobber a good snapshot.
func Sync(ctx context.Context, o SyncOptions) (SyncResult, error) {
	if o.NSMRSSURL == "" {
		o.NSMRSSURL = DefaultNSMRSSURL
	}
	if o.KEVURL == "" {
		o.KEVURL = DefaultKEVURL
	}
	if o.RegisterPath == "" {
		o.RegisterPath = DefaultRegisterPath
	}
	if o.KEVPath == "" {
		o.KEVPath = DefaultKEVPath
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	var res SyncResult
	var nsmErr, kevErr error
	if !o.SkipNSM {
		nsmErr = syncNSM(ctx, client, o, &res)
	}
	if !o.SkipKEV {
		kevErr = syncKEV(ctx, client, o, &res)
	}
	return res, errors.Join(nsmErr, kevErr)
}

// syncNSM fetches the NSM RSS feed, merges against the register file and
// appends new entries. Any error leaves the register untouched.
func syncNSM(ctx context.Context, client *http.Client, o SyncOptions, res *SyncResult) error {
	rssBody, err := fetch(ctx, client, o.NSMRSSURL, nsmRSSMaxBytes,
		"fetch NSM RSS feed", "NSM RSS feed returned HTTP %d")
	if err != nil {
		return err
	}
	recs, err := ParseNSMRSS(rssBody)
	if err != nil {
		return err
	}
	var existing []RegisterEntry
	raw, err := os.ReadFile(o.RegisterPath)
	if err == nil {
		existing, err = ReadRegister(bytes.NewReader(raw))
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return locale.Errorf("read register: %v", err)
	}
	merged := MergeRegister(existing, recs, o.Seen)
	if len(merged) > 0 {
		f, err := os.OpenFile(o.RegisterPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return locale.Errorf("read register: %v", err)
		}
		defer f.Close()
		if err := AppendRegister(f, merged); err != nil {
			return err
		}
	}
	res.NSMAdded = len(merged)
	res.NSMTotal = len(existing) + len(merged)
	return nil
}

// syncKEV fetches the KEV catalog, validates it BEFORE touching the
// existing snapshot, then writes atomically only if the catalog version
// changed. Any error leaves the snapshot untouched.
func syncKEV(ctx context.Context, client *http.Client, o SyncOptions, res *SyncResult) error {
	kevBody, err := fetch(ctx, client, o.KEVURL, kevMaxBytes,
		"fetch CISA KEV catalog", "CISA KEV catalog returned HTTP %d")
	if err != nil {
		return err
	}
	version, kevRecs, err := ParseKEV(kevBody)
	if err != nil {
		return err
	}
	res.KEVVersion = version
	res.KEVCount = len(kevRecs)
	oldVersion, err := existingKEVVersion(o.KEVPath)
	if err != nil {
		return err
	}
	if oldVersion == version {
		return nil
	}
	if err := writeKEVSnapshot(o.KEVPath, kevBody); err != nil {
		return err
	}
	res.KEVUpdated = true
	return nil
}

// fetch GETs url with the sync User-Agent, caps the body at maxBytes, and
// returns the bytes. A non-200 status is an error carrying the status via
// statusFmt (an "HTTP %d" template).
func fetch(ctx context.Context, client *http.Client, url string, maxBytes int64, opFmt, statusFmt string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, locale.Errorf("%s: %w", opFmt, err)
	}
	req.Header.Set("User-Agent", "ScopeWeaver-advisory-sync")
	resp, err := client.Do(req)
	if err != nil {
		return nil, locale.Errorf("%s: %w", opFmt, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, locale.Errorf(statusFmt, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, locale.Errorf("%s: %w", opFmt, err)
	}
	return b, nil
}

// existingKEVVersion returns the catalogVersion of the snapshot file on
// disk, or "" if the file does not exist yet. A present-but-unparseable
// snapshot is an error: the sync must not overwrite a file it cannot read.
func existingKEVVersion(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", locale.Errorf("read CISA KEV snapshot %s: %w", path, err)
	}
	var cat kevCatalog
	if err := json.Unmarshal(b, &cat); err != nil {
		return "", locale.Errorf("read CISA KEV snapshot %s: %w", path, err)
	}
	return cat.CatalogVersion, nil
}

// writeKEVSnapshot replaces the snapshot atomically: the new bytes go to
// a temp file in the same directory, then os.Rename swaps them in, so a
// crash mid-write never leaves a half-written snapshot.
func writeKEVSnapshot(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kev-*.tmp")
	if err != nil {
		return locale.Errorf("write CISA KEV snapshot %s: %w", path, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return locale.Errorf("write CISA KEV snapshot %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return locale.Errorf("write CISA KEV snapshot %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return locale.Errorf("write CISA KEV snapshot %s: %w", path, err)
	}
	return nil
}
