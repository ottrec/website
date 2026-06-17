package ottrectm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/ottrec/website/pkg/ottrecdata"
	"github.com/ottrec/website/pkg/ottrecidx"
)

// Dataset is one loaded historical snapshot.
type Dataset struct {
	ID        string            // opaque cache identifier
	Updated   time.Time         // most recent facility update timestamp in the dataset
	Committed time.Time         // when the snapshot was committed
	Data      ottrecidx.DataRef // indexed data, backed by the store's indexer
}

// Store holds the loaded datasets, newest first, sharing a single indexer so
// memory is deduplicated across snapshots. It loads all datasets up to maxAge
// old up-front and keeps only the newest revision of each update. It polls for
// new data, incrementally folding in datasets appended at the front, and fully
// reloads (reopening the cache, dropping aged-out datasets, and freeing memory)
// when the day rolls over in the schedule timezone or when the history diverges.
type Store struct {
	path   string
	maxAge time.Duration

	// cache is only accessed from the single reload/update goroutine (and from
	// Open before that goroutine starts), so it isn't guarded by mu.
	cache *ottrecdata.Cache

	mu    sync.RWMutex
	dxr   *ottrecidx.Indexer
	sets  []Dataset                // newest first (by Updated)
	seq   []string                 // full version-id sequence (newest first, incl. superseded) from the last load
	mags  []int                    // cached per-snapshot change magnitudes, aligned with sets (see Magnitudes)
	stats map[string]FacilityStats // cached per-facility change stats (see FacilityChangeStats)
	cats  [][]CategoryBreakdown    // cached per-snapshot per-category breakdowns, aligned with sets (see CategoryStats)
}

// Open loads all datasets up to maxAge old from the ottrecdata cache at path
// (opened read-only), and starts polling for updates in the background until ctx
// is cancelled. The initial load is synchronous, so once Open returns the data
// is ready to serve.
func Open(ctx context.Context, path string, maxAge time.Duration) (*Store, error) {
	s := &Store{
		path:   path,
		maxAge: maxAge,
	}

	slog.Info("timemachine: loading data", "path", path, "max_age", maxAge)
	if err := s.reload(ctx); err != nil {
		s.mu.Lock()
		if s.cache != nil {
			s.cache.Close()
		}
		s.mu.Unlock()
		return nil, err
	}

	go s.run(ctx)
	return s, nil
}

// Close closes the underlying cache.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		return nil
	}
	return s.cache.Close()
}

// Datasets returns a copy of the loaded dataset metadata, newest first.
func (s *Store) Datasets() []Dataset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Dataset(nil), s.sets...)
}

// Magnitudes returns a copy of the cached per-snapshot change magnitudes,
// aligned with [Store.Datasets] (see [Magnitudes]).
func (s *Store) Magnitudes() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]int(nil), s.mags...)
}

// FacilityStats returns a copy of the cached per-facility change stats (see
// [FacilityChangeStats]).
func (s *Store) FacilityStats() map[string]FacilityStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]FacilityStats, len(s.stats))
	for k, v := range s.stats {
		out[k] = v
	}
	return out
}

// CategoryStats returns a copy of the cached per-snapshot per-category
// breakdowns, aligned with [Store.Datasets] (see [CategoryStats]).
func (s *Store) CategoryStats() [][]CategoryBreakdown {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([][]CategoryBreakdown, len(s.cats))
	for i, row := range s.cats {
		out[i] = append([]CategoryBreakdown(nil), row...)
	}
	return out
}

// run polls for new data every 15s, and fully reloads whenever the day rolls
// over in the schedule timezone. It returns when ctx is cancelled.
func (s *Store) run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	day := today()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d := today(); d != day {
				day = d
				slog.Info("timemachine: day changed, reloading all data")
				if err := s.reload(ctx); err != nil {
					slog.Error("timemachine: failed to reload data", "error", err)
				}
			} else {
				if err := s.update(ctx); err != nil {
					slog.Error("timemachine: failed to update data", "error", err)
				}
			}
		}
	}
}

// reload reopens the cache and loads everything fresh, replacing all state. It
// blocks readers for the whole duration.
func (s *Store) reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked(ctx)
}

// reloadLocked reopens the cache, scans it, and loads everything fresh into a new
// indexer, replacing all state. It frees the old indexer's memory before loading
// to avoid briefly holding two full copies of the data. s.mu must be held.
func (s *Store) reloadLocked(ctx context.Context) error {
	// reopen the cache so a schema change (e.g. ottrec-data was redeployed and
	// reset the database) is picked up and validated rather than read blindly.
	if err := s.reopen(); err != nil {
		return err
	}

	seq, desired, err := s.scan(ctx)
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}

	s.dxr = nil
	s.sets = nil
	s.seq = nil
	s.cats = nil
	debug.FreeOSMemory()

	dxr := new(ottrecidx.Indexer)
	sets := make([]Dataset, 0, len(desired))
	for _, ver := range desired {
		ds, err := s.load(ctx, dxr, ver)
		if err != nil {
			return fmt.Errorf("load %s: %w", ver.ID, err)
		}
		sets = append(sets, ds)
	}

	s.dxr = dxr
	s.sets = sets
	s.seq = seq
	s.mags = Magnitudes(sets)
	s.stats = FacilityChangeStats(sets)
	s.cats = CategoryStats(sets)
	slog.Info("timemachine: loaded data", "datasets", len(sets), "versions", len(seq))
	return nil
}

// update reconciles the loaded datasets with the current cache contents. As long
// as the previously-recorded version sequence (including superseded revisions)
// is still a suffix of the freshly-scanned one, the only change is new data
// appended at the front, so the desired datasets are folded into the existing
// indexer incrementally (loading new ids, reusing loaded ones, and dropping any
// now superseded by a newer revision that just appeared). If the recorded
// sequence is no longer a suffix, the underlying history diverged (something
// older was rewritten or removed), so everything is reloaded from scratch.
func (s *Store) update(ctx context.Context) error {
	seq, desired, err := s.scan(ctx)
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dxr == nil {
		return nil // not loaded yet
	}

	if !isSuffix(s.seq, seq) {
		slog.Info("timemachine: history diverged, reloading all data")
		return s.reloadLocked(ctx)
	}
	if slices.Equal(s.seq, seq) {
		return nil // unchanged
	}

	prev := make(map[string]Dataset, len(s.sets))
	for _, ds := range s.sets {
		prev[ds.ID] = ds
	}

	var loaded int
	sets := make([]Dataset, 0, len(desired))
	for _, ver := range desired {
		if ds, ok := prev[ver.ID]; ok {
			sets = append(sets, ds)
			continue
		}
		ds, err := s.load(ctx, s.dxr, ver)
		if err != nil {
			return fmt.Errorf("load %s: %w", ver.ID, err)
		}
		sets = append(sets, ds)
		loaded++
	}

	s.sets = sets
	s.seq = seq
	s.mags = Magnitudes(sets)
	s.stats = FacilityChangeStats(sets)
	s.cats = CategoryStats(sets)
	slog.Info("timemachine: updated datasets", "loaded", loaded, "total", len(sets))
	return nil
}

// reopen closes the current cache (if any) and opens a fresh read-only handle,
// so a changed schema is revalidated. s.mu must be held.
func (s *Store) reopen() error {
	cache, err := ottrecdata.OpenCacheReadOnly(s.path)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	if s.cache != nil {
		s.cache.Close()
	}
	s.cache = cache
	return nil
}

// isSuffix reports whether suf is a suffix of s.
func isSuffix[T comparable](suf, s []T) bool {
	return len(suf) <= len(s) && slices.Equal(suf, s[len(s)-len(suf):])
}

// cutoff returns the oldest Updated timestamp to keep, anchored to the start of
// the current day so the window only shifts on the day-change reload.
func (s *Store) cutoff() time.Time {
	now := time.Now().In(ottrecidx.TZ)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ottrecidx.TZ)
	return start.Add(-s.maxAge)
}

// scan reads the cache versions within the age window, newest first. It returns
// the full sequence of version ids (including superseded older revisions, for
// detecting whether the history diverged) and the deduplicated versions to
// actually load (the newest revision of each update timestamp).
func (s *Store) scan(ctx context.Context) (seq []string, desired []ottrecdata.DataVersion, err error) {
	cutoff := s.cutoff()
	var last time.Time
	// DataVersions yields newest first (updated DESC, revision DESC), so the
	// first entry of each identical-Updated run is the newest revision.
	for ver := range s.cache.DataVersions(ctx)(&err) {
		if ver.Updated.Before(cutoff) {
			break // sorted descending, everything remaining is older
		}
		seq = append(seq, ver.ID)
		if len(desired) != 0 && ver.Updated.Equal(last) {
			continue // superseded: keep only the newest revision of each update
		}
		desired = append(desired, ver)
		last = ver.Updated
	}
	return seq, desired, err
}

// load reads and indexes a single dataset into dxr.
func (s *Store) load(ctx context.Context, dxr *ottrecidx.Indexer, ver ottrecdata.DataVersion) (Dataset, error) {
	pb, err := s.readPB(ctx, ver.ID)
	if err != nil {
		return Dataset{}, err
	}
	idx, err := dxr.Load(pb)
	if err != nil {
		return Dataset{}, fmt.Errorf("index: %w", err)
	}
	return Dataset{
		ID:        ver.ID,
		Updated:   ver.Updated,
		Committed: ver.Committed,
		Data:      idx.Data(),
	}, nil
}

// readPB reads the binary protobuf for a cache version.
func (s *Store) readPB(ctx context.Context, id string) ([]byte, error) {
	var (
		hash string
		err  error
	)
	for h, format := range s.cache.DataFormats(ctx, id)(&err) {
		if format == "pb" {
			hash = h
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("list formats: %w", err)
	}
	if hash == "" {
		return nil, fmt.Errorf("no pb format for %s", id)
	}

	var pb []byte
	ok, err := s.cache.ReadBlob(ctx, hash, false, func(r io.Reader, _ int64) error {
		var err error
		pb, err = io.ReadAll(r)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", hash, err)
	}
	if !ok {
		return nil, fmt.Errorf("blob %s missing", hash)
	}
	return pb, nil
}

// today returns the current date in the schedule timezone.
func today() string {
	return time.Now().In(ottrecidx.TZ).Format("2006-01-02")
}
