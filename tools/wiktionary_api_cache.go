//go:build !greektranslit

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultCacheDir = ".cache"
	defaultCacheTTL = 30 * 24 * time.Hour
)

var (
	apiCacheDir string
	apiCacheTTL = defaultCacheTTL
	apiCacheNow = time.Now
)

type apiCacheFlags struct {
	dir *string
	ttl *time.Duration
}

func registerAPICacheFlags() apiCacheFlags {
	return apiCacheFlags{
		dir: flag.String("cache-dir", defaultCacheDir, "directory for cached Wiktionary API responses; empty disables caching"),
		ttl: flag.Duration("cache-ttl", defaultCacheTTL, "cached Wiktionary API response freshness duration; 0 always revalidates"),
	}
}

func configureAPICache(flags apiCacheFlags) error {
	if *flags.ttl < 0 {
		return fmt.Errorf("-cache-ttl must be >= 0")
	}
	apiCacheDir = strings.TrimSpace(*flags.dir)
	apiCacheTTL = *flags.ttl
	return nil
}

func readCachedAPIResponse(rawURL string) ([]byte, bool) {
	if apiCacheDir == "" || apiCacheTTL == 0 {
		return nil, false
	}
	path := apiCachePath(rawURL)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if apiCacheTTL > 0 && apiCacheNow().Sub(info.ModTime()) > apiCacheTTL {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 || string(data[:lineEnd]) != rawURL {
		return nil, false
	}
	return data[lineEnd+1:], true
}

func writeCachedAPIResponse(rawURL string, body []byte) {
	if apiCacheDir == "" {
		return
	}
	if err := os.MkdirAll(apiCacheDir, 0755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(apiCacheDir, ".api-cache-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()

	var writeErr error
	if _, err := tmp.WriteString(rawURL + "\n"); err != nil {
		writeErr = err
	}
	if writeErr == nil {
		if _, err := tmp.Write(body); err != nil {
			writeErr = err
		}
	}
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, apiCachePath(rawURL)); err != nil {
		os.Remove(tmpName)
	}
}

func apiCachePath(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return filepath.Join(apiCacheDir, hex.EncodeToString(sum[:]))
}
