package companioninstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// download fetches the official asset into stage, verifies the published
// SHA-256, and returns the verified file path. Transient network and server
// errors are retried; a checksum or size mismatch is never retried.
func (i *Installer) download(ctx context.Context, pkg Package, stage string) (string, error) {
	ctx, cancel := i.ttl(ctx, i.downloadTimeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= maxDownloadAttempt; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(i.retryDelay(attempt - 1)):
			}
		}
		path, retry, err := i.downloadOnce(ctx, pkg, stage)
		if err == nil {
			return path, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("companioninstall: download failed after %d attempts: %w", maxDownloadAttempt, lastErr)
}

func (i *Installer) downloadOnce(ctx context.Context, pkg Package, stage string) (path string, retry bool, err error) {
	resp, err := i.get(ctx, i.http, pkg.OfficialAssetURL, "application/octet-stream")
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", true, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", true, fmt.Errorf("companioninstall: download returned HTTP %d", resp.StatusCode)
	default:
		return "", false, fmt.Errorf("companioninstall: download returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > pkg.SizeCap {
		return "", false, fmt.Errorf("companioninstall: official asset reports %d bytes, above the %d byte cap", resp.ContentLength, pkg.SizeCap)
	}
	if pkg.ExpectedBytes > 0 && resp.ContentLength > 0 && resp.ContentLength != pkg.ExpectedBytes {
		return "", false, fmt.Errorf("companioninstall: official asset reports %d bytes but official metadata says %d", resp.ContentLength, pkg.ExpectedBytes)
	}

	part := filepath.Join(stage, "asset.part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", false, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, pkg.SizeCap+1))
	closeErr := f.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", true, fmt.Errorf("companioninstall: download interrupted after %d bytes: %w", n, copyErr)
	}
	if closeErr != nil {
		return "", false, closeErr
	}
	if n > pkg.SizeCap {
		return "", false, fmt.Errorf("companioninstall: download exceeded the %d byte cap", pkg.SizeCap)
	}
	if pkg.ExpectedBytes > 0 && n != pkg.ExpectedBytes {
		return "", false, fmt.Errorf("companioninstall: downloaded %d bytes but official metadata says %d", n, pkg.ExpectedBytes)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, pkg.ExpectedSHA256) {
		return "", false, fmt.Errorf("companioninstall: SHA-256 mismatch for %s: official %s, downloaded %s", pkg.Name, pkg.ExpectedSHA256, got)
	}

	final := filepath.Join(stage, "asset")
	if err := os.Rename(part, final); err != nil {
		return "", false, err
	}
	return final, false, nil
}
