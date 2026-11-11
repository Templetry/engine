// Package safepath is the single gate every file write goes through. A
// template is untrusted input (a registry can point anywhere), so the path of
// each file it ships is checked against the destination root before anything
// touches the disk: no escapes, no absolute paths, nothing under a .git
// directory (a hook there is code execution on the next commit), and no
// writes through symlinks.
//
// Checks that depend on the platform's file-name rules (case folding, 8.3
// short names, trailing dots and spaces) are applied on every OS: a project
// rendered on Linux is routinely checked out on Windows or macOS.
package safepath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrEscape marks a path that is absolute, climbs out of the root or uses
// separator syntax that some platform would read as either.
var ErrEscape = errors.New("path escapes the destination root")

// shortGit matches the 8.3 short name NTFS may give a ".git" entry (GIT~1).
var shortGit = regexp.MustCompile(`^git~[0-9]+$`)

// reserved are the Windows device names; they are not creatable as files
// there, with or without an extension.
var reserved = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("COM%d", i)] = true
		m[fmt.Sprintf("LPT%d", i)] = true
	}
	return m
}()

// IsGitName reports whether a single path segment names a git directory on
// any mainstream file system: ".git" in any case, with trailing dots or
// spaces (ignored by Windows), the 8.3 short name "git~1", or with the
// zero-width code points HFS+ ignores.
func IsGitName(seg string) bool {
	seg = strings.Map(func(r rune) rune {
		switch {
		case r == 0x200c, r == 0x200d, r == 0x200e, r == 0x200f,
			r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		}
		return r
	}, seg)
	seg = strings.ToLower(strings.TrimRight(seg, ". "))
	return seg == ".git" || shortGit.MatchString(seg)
}

// ValidateRel checks a template-relative, slash-separated path.
func ValidateRel(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character", ErrEscape)
		}
	}
	if strings.ContainsAny(p, `\:`) {
		return fmt.Errorf("%w: backslash or colon", ErrEscape)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: absolute path", ErrEscape)
	}
	c := pathpkg.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("%w: climbs out of the root", ErrEscape)
	}
	if c == "." {
		return nil
	}
	for _, seg := range strings.Split(c, "/") {
		if IsGitName(seg) {
			return fmt.Errorf("segment %q is a git directory", seg)
		}
		if seg != strings.TrimRight(seg, ". ") {
			return fmt.Errorf("segment %q ends in a dot or space (silently trimmed on Windows)", seg)
		}
		base := seg
		if i := strings.IndexByte(seg, '.'); i >= 0 {
			base = seg[:i]
		}
		if reserved[strings.ToUpper(strings.TrimRight(base, " "))] {
			return fmt.Errorf("segment %q is a reserved Windows device name", seg)
		}
	}
	return nil
}

// NoLinks fails when root/rel, or any directory between them, is a symlink
// or another reparse point. Components that do not exist yet are fine: the
// writer is about to create them. The root itself is the caller's choice and
// is not inspected.
//
// This is a check, not a lock: a concurrent process able to swap a directory
// for a symlink between the check and the write can still win the race.
func NoLinks(root, rel string) error {
	cur := root
	for _, seg := range strings.Split(pathpkg.Clean(rel), "/") {
		if seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("%s is a symlink or reparse point", seg)
		}
	}
	return nil
}

// Resolve joins rel under root after every check above and returns the
// absolute destination. It is the only way the engine turns a template path
// into a file system path.
func Resolve(root, rel string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ValidateRel(rel); err != nil {
		if errors.Is(err, ErrEscape) {
			return "", fmt.Errorf("refusing to write outside %s: %q (%w)", root, rel, err)
		}
		return "", fmt.Errorf("refusing to write %q: %w", rel, err)
	}
	full := filepath.Join(abs, filepath.FromSlash(rel))
	r, err := filepath.Rel(abs, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write outside %s: %q", root, rel)
	}
	if err := NoLinks(abs, rel); err != nil {
		return "", fmt.Errorf("refusing to write %q: %w", rel, err)
	}
	return full, nil
}
