package transport

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// multipartSpillPrefix names the per-upload spill directories created for
// multipart parts (issue #1358).
const multipartSpillPrefix = "momo-s3-multipart-"

// multipartSpillMarker names the file written into every spill directory this
// process creates. The sweep only removes directories carrying it, so a
// look-alike directory in a shared temp location is never deleted (it may belong
// to another user or another program).
const multipartSpillMarker = ".momo-multipart-spill"

// createMultipartSpillDir makes a private (0700) spill directory and stamps it
// with the ownership marker.
func createMultipartSpillDir() (string, error) {
	dir, err := os.MkdirTemp("", multipartSpillPrefix+"*")
	if err != nil {
		return "", err
	}
	if werr := os.WriteFile(filepath.Join(dir, multipartSpillMarker), nil, 0o600); werr != nil {
		_ = os.RemoveAll(dir)
		return "", werr
	}
	return dir, nil
}

// multipartReapInterval is how often expired sessions and orphaned spill
// directories are swept. The TTL is the policy; this is only the granularity.
const multipartReapInterval = 10 * time.Minute

// ReapExpiredMultipartUploads drops incomplete multipart sessions older than ttl
// along with their spooled parts, and removes spill directories left behind by a
// daemon that died mid-upload. Returns how many sessions and directories were
// removed. A ttl <= 0 disables reaping entirely.
func ReapExpiredMultipartUploads(ttl time.Duration, now time.Time, tempRoot string) (sessions, orphans int) {
	if ttl <= 0 {
		return 0, 0
	}
	cutoff := now.Add(-ttl)

	muUploads.Lock()
	for id, up := range uploads {
		if up.createdAt.After(cutoff) {
			continue
		}
		up.mu.Lock()
		dir := up.spillDir
		up.mu.Unlock()
		delete(uploads, id)
		removeSpill(dir)
		sessions++
		log.Printf("AUDIT: multipart upload %s expired after %s (bucket=%s key=%s)", id, ttl, up.bucket, up.key)
	}
	muUploads.Unlock()

	if tempRoot == "" {
		tasks, err := filepath.Glob(filepath.Join(os.TempDir(), multipartSpillPrefix+"*"))
		if err != nil {
			log.Printf("AUDIT: multipart spill scan failed: %v", err)
			return sessions, 0
		}
		orphans = removeOrphanSpills(tasks, cutoff)
		return sessions, orphans
	}

	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		log.Printf("AUDIT: multipart spill scan of %s failed: %v", tempRoot, err)
		return sessions, 0
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), multipartSpillPrefix) {
			paths = append(paths, filepath.Join(tempRoot, e.Name()))
		}
	}
	return sessions, removeOrphanSpills(paths, cutoff)
}

// removeOrphanSpills deletes spill directories that are not referenced by a live
// session and whose modification time is older than cutoff. A directory that a
// live session still owns is left alone so an in-flight upload is never
// destroyed.
func removeOrphanSpills(paths []string, cutoff time.Time) int {
	muUploads.Lock()
	live := make(map[string]struct{}, len(uploads))
	for _, up := range uploads {
		up.mu.Lock()
		if up.spillDir != "" {
			live[up.spillDir] = struct{}{}
		}
		up.mu.Unlock()
	}
	muUploads.Unlock()

	removed := 0
	for _, p := range paths {
		if _, ok := live[p]; ok {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.ModTime().After(cutoff) {
			continue
		}
		// Ownership proof: only directories this process stamped are ours to
		// delete. Anything else in a shared temp root is left alone.
		if _, mErr := os.Lstat(filepath.Join(p, multipartSpillMarker)); mErr != nil {
			continue
		}
		removeSpill(p)
		removed++
	}
	if removed > 0 {
		log.Printf("AUDIT: removed %d orphaned multipart spill director(ies)", removed)
	}
	return removed
}

// StartMultipartReaper runs the expiry sweep at startup and then periodically
// until ctx is cancelled. It returns a channel closed when the loop has exited,
// so shutdown (and tests) can wait for a clean stop.
func StartMultipartReaper(ctx context.Context, ttl time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if ttl <= 0 {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("AUDIT: multipart reaper panicked: %v", r)
			}
		}()

		sessions, orphans := ReapExpiredMultipartUploads(ttl, time.Now(), "")
		if sessions+orphans > 0 {
			log.Printf("AUDIT: multipart reaper start: %d session(s), %d orphan(s)", sessions, orphans)
		}

		ticker := time.NewTicker(multipartReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ReapExpiredMultipartUploads(ttl, time.Now(), "")
			}
		}
	}()
	return done
}

// MultipartTTLFromConfig parses the configured TTL, falling back to the default
// when the value is unusable (the loader rejects bad values, so this is a
// belt-and-braces path for programmatic configuration).
func MultipartTTLFromConfig(raw string) time.Duration {
	if raw == "" {
		return 24 * time.Hour
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("AUDIT: invalid multipart_upload_ttl %q; using 24h: %v", raw, err)
		return 24 * time.Hour
	}
	return ttl
}
