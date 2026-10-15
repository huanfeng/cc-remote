package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// selectAsset picks the release archive for goos/goarch. Release archives are
// named cc-remote_<version>_<goos>_<goarch>.zip (Windows) or .tar.gz.
func selectAsset(assets []Asset, goos, goarch string) (Asset, error) {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	suffix := "_" + goos + "_" + goarch + ext
	for _, a := range assets {
		if strings.HasPrefix(a.Name, "cc-remote_") && strings.HasSuffix(a.Name, suffix) {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("release has no asset for %s/%s (expected cc-remote_<version>%s)", goos, goarch, suffix)
}

func findAsset(assets []Asset, name string) (Asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// parseChecksums reads sha256sum-style lines: "<hex>  <name>" or "<hex> *<name>".
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed checksum line %q", line)
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != sha256.Size*2 {
			return nil, fmt.Errorf("malformed sha256 %q", fields[0])
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return nil, fmt.Errorf("malformed sha256 %q", fields[0])
		}
		sums[strings.TrimPrefix(fields[1], "*")] = sum
	}
	return sums, sc.Err()
}

func verifyFile(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", filepath.Base(path), got, want)
	}
	return nil
}

// safeJoin resolves an archive entry name inside dir, rejecting absolute paths
// and ".." components that would escape it.
func safeJoin(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(name, `\`, "/")))
	if clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" ||
		clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return filepath.Join(dir, clean), nil
}

func writeEntry(target string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func extractZip(archive, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue // skip symlinks and other special entries
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeEntry(target, rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeEntry(target, tr, 0o755); err != nil {
				return err
			}
		default:
			// symlinks, hard links and devices are never part of a release archive
		}
	}
}

// extractArchive unpacks a .zip or .tar.gz into dst and returns the path of
// the cc-remote binary inside it.
func extractArchive(archive, dst, goos string) (string, error) {
	var err error
	switch {
	case strings.HasSuffix(archive, ".zip"):
		err = extractZip(archive, dst)
	case strings.HasSuffix(archive, ".tar.gz"):
		err = extractTarGz(archive, dst)
	default:
		err = fmt.Errorf("unsupported archive %s", filepath.Base(archive))
	}
	if err != nil {
		return "", err
	}
	name := "cc-remote"
	if goos == "windows" {
		name += ".exe"
	}
	var found string
	err = filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name && found == "" {
			found = p
		}
		return err
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("archive %s does not contain %s", filepath.Base(archive), name)
	}
	return found, nil
}
