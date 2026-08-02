package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func makeTarGz(t *testing.T, files map[string]struct {
	data string
	mode int64
}) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, f := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.data)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return &buf
}

func fixture(t *testing.T) *bytes.Buffer {
	return makeTarGz(t, map[string]struct {
		data string
		mode int64
	}{
		"kmp-main/README.md":                 {"root readme", 0o644},
		"kmp-main/modular-features/a.txt":    {"aa", 0o644},
		"kmp-main/modular-features/gradlew":  {"#!/bin/sh", 0o755},
		"kmp-main/single-module/b.txt":       {"bb", 0o644},
		"kmp-main/modular-features/logo.png": {"\x00\x01\x02", 0o644},
	})
}

func TestFromTarGzWholeRepo(t *testing.T) {
	set, err := FromTarGz(fixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if set.Get("README.md") == nil || set.Get("modular-features/a.txt") == nil {
		t.Fatalf("top-level dir not stripped, paths: %v", set.Paths())
	}
}

func TestFromTarGzSubdir(t *testing.T) {
	set, err := FromTarGz(fixture(t), "modular-features")
	if err != nil {
		t.Fatal(err)
	}
	if set.Get("a.txt") == nil {
		t.Fatalf("subdir prefix not removed, paths: %v", set.Paths())
	}
	if set.Get("b.txt") != nil || set.Get("README.md") != nil {
		t.Error("files outside the subdir must be excluded")
	}
	if g := set.Get("gradlew"); g == nil || !g.Exec {
		t.Error("exec bit must survive from tar headers")
	}
	if b := set.Get("logo.png"); b == nil || !b.Binary {
		t.Error("binary detection must apply to tarball files")
	}
}

func TestFromTarGzWrongSubdir(t *testing.T) {
	if _, err := FromTarGz(fixture(t), "nope"); err == nil {
		t.Error("empty subdir selection must error")
	}
}
