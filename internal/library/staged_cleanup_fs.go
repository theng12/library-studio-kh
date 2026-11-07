package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"librarystudio/internal/stock"
)

// CleanupFile is one verified file of a staged download, relative to the item
// root that gets moved. It is persisted so later stages re-check the same
// content instead of trusting a path or title.
type CleanupFile struct {
	Rel    string `json:"rel"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func sameFileSnapshot(before, after os.FileInfo) bool {
	return after.Mode().IsRegular() && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if n != before.Size() || !sameFileSnapshot(before, after) {
		return "", errors.New("file changed during checksum verification")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// observedTree lists regular files under root (or root itself) and hashes them.
// Symlinks and other non-regular entries are refused, never followed.
func observedTree(root string) (map[string]CleanupFile, error) {
	fi, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symlink", root)
	}
	if fi.Mode().IsRegular() {
		sum, err := hashFile(root)
		if err != nil {
			return nil, err
		}
		// The item itself uses a root-independent key: the destination's
		// basename changes when it moves into _trash or the system Trash.
		return map[string]CleanupFile{"": {Rel: "", Size: fi.Size(), SHA256: sum}}, nil
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a regular file or folder", root)
	}
	out := map[string]CleanupFile{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		out[rel] = CleanupFile{Rel: rel, Size: info.Size(), SHA256: sum}
		return nil
	})
	return out, err
}

// verifyTree proves root still contains exactly the recorded files and bytes.
func verifyTree(root string, expected []CleanupFile) error {
	got, err := observedTree(root)
	if err != nil {
		return err
	}
	if len(got) != len(expected) {
		return fmt.Errorf("%s has %d files, expected %d", root, len(got), len(expected))
	}
	for _, e := range expected {
		g, ok := got[e.Rel]
		if !ok || g.Size != e.Size || g.SHA256 != e.SHA256 {
			return fmt.Errorf("%s doesn't match the verified copy", root)
		}
	}
	return nil
}

func treeSignature(files []CleanupFile) string {
	sorted := append([]CleanupFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rel < sorted[j].Rel })
	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00", f.Rel, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sourceLayout verifies the torrent's own files live on a local volume inside
// its save path and that moving the returned root cannot take unrelated user
// data with it. It reads and hashes each file once.
func sourceLayout(t stock.CleanupTorrent, fs []stock.TorrentFile, local func(string) (bool, error)) (root string, files []CleanupFile, bytes int64, err error) {
	if strings.TrimSpace(t.SavePath) == "" {
		return "", nil, 0, errors.New("qBittorrent reports no save path")
	}
	save, err := filepath.Abs(t.SavePath)
	if err != nil {
		return "", nil, 0, err
	}
	fi, err := os.Lstat(save)
	if err != nil {
		return "", nil, 0, fmt.Errorf("the download folder can't be read: %w", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return "", nil, 0, errors.New("the download folder isn't a real folder")
	}
	ok, err := local(save)
	if err != nil {
		return "", nil, 0, err
	}
	if !ok {
		return "", nil, 0, errors.New("the download folder is not on a local volume")
	}
	if len(fs) == 0 {
		return "", nil, 0, errors.New("qBittorrent lists no files for this torrent")
	}

	type item struct {
		name string
		size int64
		rel  string
	}
	items := make([]item, 0, len(fs))
	top := ""
	for _, f := range fs {
		if f.Progress < 1 {
			return "", nil, 0, errors.New("at least one file is unfinished")
		}
		clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(f.Name)))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return "", nil, 0, fmt.Errorf("qBittorrent reported an unsafe file name %q", f.Name)
		}
		first := strings.Split(filepath.ToSlash(clean), "/")[0]
		if top == "" {
			top = first
		} else if first != top {
			return "", nil, 0, errors.New("the torrent has more than one top-level item")
		}
		items = append(items, item{name: filepath.ToSlash(clean), size: f.Size})
	}

	if len(fs) == 1 && !strings.Contains(items[0].name, "/") {
		root = filepath.Join(save, filepath.FromSlash(items[0].name))
	} else {
		root = filepath.Join(save, filepath.FromSlash(top))
	}
	seen, err := observedTree(root)
	if err != nil {
		return "", nil, 0, fmt.Errorf("the local copy can't be verified: %w", err)
	}
	for i := range items {
		if items[i].name == top {
			items[i].rel = "" // the root item itself; independent of its basename
		} else {
			items[i].rel = strings.TrimPrefix(items[i].name, top+"/")
		}
		got, ok := seen[items[i].rel]
		if !ok {
			return "", nil, 0, fmt.Errorf("the local copy is missing %s", items[i].rel)
		}
		if got.Size != items[i].size {
			return "", nil, 0, fmt.Errorf("%s isn't the size qBittorrent reported", items[i].rel)
		}
		files = append(files, got)
		bytes += got.Size
	}
	if len(seen) != len(items) {
		return "", nil, 0, errors.New("the local folder holds files this torrent didn't report; it is left protected")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return root, files, bytes, nil
}

// proveImported checks every staged file's bytes exist under the library path
// the app reported for this download ID. Names may change during import, so it
// matches content, never titles.
func proveImported(importedPath string, files []CleanupFile) error {
	p, err := filepath.Abs(importedPath)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("the library copy can't be read: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("the library copy is a symlink")
	}
	if fi.Mode().IsRegular() {
		if len(files) != 1 || fi.Size() != files[0].Size {
			return errors.New("the library file doesn't match the download")
		}
		sum, err := hashFile(p)
		if err != nil {
			return err
		}
		if sum != files[0].SHA256 {
			return errors.New("the library file content doesn't match the download")
		}
		return nil
	}
	if !fi.IsDir() {
		return errors.New("the library copy isn't a file or folder")
	}
	bySize := map[int64][]string{}
	err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		bySize[info.Size()] = append(bySize[info.Size()], path)
		return nil
	})
	if err != nil {
		return err
	}
	for _, want := range files {
		found := false
		for _, candidate := range bySize[want.Size] {
			sum, err := hashFile(candidate)
			if err != nil {
				return err
			}
			if sum == want.SHA256 {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("the library copy is missing verified content for %s", want.Rel)
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		return copyFile(path, target, 0o600)
	})
}

// moveVerified moves src to dest only after both copies match the recorded
// content. Across filesystems it copies, verifies, renames into place and only
// then removes the source. An interrupted earlier move is adopted safely.
func moveVerified(src, dest string, expected []CleanupFile) error {
	if err := verifyTree(src, expected); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		if err := verifyTree(dest, expected); err != nil {
			return fmt.Errorf("%s already exists and does not match; nothing was changed", dest)
		}
		return removeVerified(src, expected)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err == nil {
		if err := verifyTree(dest, expected); err != nil {
			return fmt.Errorf("moved, but the destination couldn't be verified: %w", err)
		}
		return nil
	}
	if err := copyItem(src, dest); err != nil {
		return err
	}
	if err := verifyTree(dest, expected); err != nil {
		os.RemoveAll(dest) // this dest was newly created; the source is untouched
		return fmt.Errorf("copied, but the destination couldn't be verified; the original is untouched: %w", err)
	}
	return removeVerified(src, expected)
}

// copyItem copies src to a temporary sibling of dest and renames it into place,
// so a partial copy never appears at dest.
func copyItem(src, dest string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", src)
	}
	parent := filepath.Dir(dest)
	if fi.IsDir() {
		tmp, err := os.MkdirTemp(parent, ".library-studio-tmp-")
		if err != nil {
			return err
		}
		if err := copyTree(src, tmp); err != nil {
			os.RemoveAll(tmp)
			return err
		}
		if err := os.Rename(tmp, dest); err != nil {
			os.RemoveAll(tmp)
			return err
		}
		return nil
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	tmp, err := os.CreateTemp(parent, ".library-studio-tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	in, err := os.Open(src)
	if err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	defer in.Close()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dest); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// removeVerified deletes only a copy whose content still matches its proof.
func removeVerified(path string, expected []CleanupFile) error {
	if err := verifyTree(path, expected); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
