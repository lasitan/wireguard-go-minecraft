package update

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

var downloadClient = &http.Client{Timeout: 10 * time.Minute}

// Download saves an asset to dir and verifies its size and sha256 digest.
func Download(ctx context.Context, a Asset, dir string, progress io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Proxied(a.URL), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "lasitan-cluster/"+orDev(Current()))
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", a.Name, resp.Status)
	}

	f, err := os.CreateTemp(dir, ".lasitan-update-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(path)
		}
	}()

	h := sha256.New()
	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{r: resp.Body, total: a.Size, out: progress}
	}
	n, err := io.Copy(io.MultiWriter(f, h), src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if progress != nil {
		fmt.Fprintln(progress)
	}
	if err != nil {
		return "", err
	}
	if a.Size > 0 && n != a.Size {
		return "", fmt.Errorf("download %s: got %d bytes, want %d", a.Name, n, a.Size)
	}
	if a.SHA256 != "" {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
			return "", fmt.Errorf("download %s: sha256 mismatch", a.Name)
		}
	}
	ok = true
	return path, nil
}

// ReplaceExecutable atomically swaps exe for the file at newPath.
// The running process keeps its old image; callers restart services after.
func ReplaceExecutable(exe, newPath string) error {
	if err := os.Chmod(newPath, 0o755); err != nil {
		return err
	}
	old := exe + ".old"
	_ = os.Remove(old)
	// Windows refuses to overwrite a running image but allows renaming it.
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("move current binary aside: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Remove(old) // fails on Windows while running; CleanupOld handles it later
	return nil
}

// CleanupOld removes the previous binary left behind by ReplaceExecutable.
func CleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	_ = os.Remove(exe + ".old")
}

type progressReader struct {
	r     io.Reader
	total int64
	done  int64
	last  int
	out   io.Writer
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.total > 0 {
		if pct := int(p.done * 100 / p.total); pct != p.last {
			p.last = pct
			fmt.Fprintf(p.out, "\r  下载中 %3d%%", pct)
		}
	}
	return n, err
}
