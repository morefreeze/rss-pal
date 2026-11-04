package summaryeval

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const suiteJSONLimit = 2 << 20

// LoadSuiteFile binds a validated suite to the SHA-256 of its exact file bytes.
// Empty expectedSHA256 is for offline inspection; the live CLI requires a guard.
// Paths must be canonical lexical paths with no symlink in any component.
func LoadSuiteFile(path, expectedSHA256 string) (Suite, string, error) {
	var empty Suite
	if expectedSHA256 != "" && !validSHA256(expectedSHA256) {
		return empty, "", errors.New("suite checksum must be 64 hexadecimal characters")
	}
	f, err := openSuiteFile(path)
	if err != nil {
		return empty, "", errors.New("suite path must name an accessible regular file without symlinks or path aliases")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > suiteJSONLimit {
		return empty, "", errors.New("suite must be a nonempty regular file at most 2 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, suiteJSONLimit+1))
	if err != nil || len(raw) == 0 || len(raw) > suiteJSONLimit {
		return empty, "", errors.New("unable to read bounded suite file")
	}
	hash := hashText(string(raw))
	if expectedSHA256 != "" && !strings.EqualFold(hash, expectedSHA256) {
		return empty, "", errors.New("suite file checksum mismatch")
	}
	suite, err := DecodeSuite(raw)
	if err != nil {
		return empty, "", err
	}
	return suite, hash, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return false
		}
	}
	return true
}

func openSuiteFile(path string) (*os.File, error) {
	if path == "" || path == "." || path == string(filepath.Separator) || path != filepath.Clean(path) {
		return nil, errors.New("noncanonical suite path")
	}
	root := "."
	if filepath.IsAbs(path) {
		root, path = string(filepath.Separator), strings.TrimPrefix(path, string(filepath.Separator))
	}
	parts := strings.Split(path, string(filepath.Separator))
	// Open every component relative to a pinned directory descriptor. Lstat
	// followed by Open would allow a concurrent symlink swap of a parent.
	dir, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { unix.Close(dir) }()
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("aliased suite path")
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(dir, part, flags, 0)
		if err != nil {
			return nil, err
		}
		if i == len(parts)-1 {
			return os.NewFile(uintptr(fd), "evaluation suite"), nil
		}
		unix.Close(dir)
		dir = fd
	}
	return nil, errors.New("missing suite file")
}
