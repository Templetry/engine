package source

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"strings"
)

// FromTarGz reads a gzipped tarball into a FileSet. The top-level directory
// (which GitHub tarballs always add) is stripped; when subdir is non-empty,
// only files under it are loaded, with the prefix removed. Execute bits come
// from the tar headers — remote templates keep them even when rendering from
// Windows.
func FromTarGz(r io.Reader, subdir string) (*FileSet, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("tarball: %w", err)
	}
	defer gz.Close()

	set := NewFileSet()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := pathpkg.Clean(hdr.Name)
		i := strings.Index(name, "/")
		if i < 0 {
			continue // top-level file outside the wrapper dir; GitHub never emits these
		}
		name = name[i+1:]
		if subdir != "" {
			rest, ok := strings.CutPrefix(name, subdir+"/")
			if !ok {
				continue
			}
			name = rest
		}
		if name == "" || strings.HasPrefix(name, "../") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tarball: reading %s: %w", name, err)
		}
		set.Put(name, &File{
			Data:   data,
			Exec:   hdr.FileInfo().Mode()&0o111 != 0,
			Binary: IsBinary(data),
		})
	}
	if set.Len() == 0 {
		if subdir != "" {
			return nil, fmt.Errorf("tarball contains no files under %q", subdir)
		}
		return nil, fmt.Errorf("tarball contains no files")
	}
	return set, nil
}

// FetchGitHubTarball downloads repo ("owner/name") at ref (branch, tag or
// commit) as a FileSet, optionally scoped to subdir.
func FetchGitHubTarball(repo, ref, subdir string) (*FileSet, error) {
	url := fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", repo, ref)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", repo, ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s@%s: HTTP %d", repo, ref, resp.StatusCode)
	}
	return FromTarGz(resp.Body, subdir)
}
