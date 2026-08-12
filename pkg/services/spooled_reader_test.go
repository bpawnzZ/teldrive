package services

import (
	"bytes"
	"io"
	"testing"
)

func TestSpooledReaderReplay(t *testing.T) {
	src := bytes.NewReader([]byte("hello teldrive part bytes"))
	sr, err := newSpooledReader(src)
	if err != nil {
		t.Fatalf("newSpooledReader: %v", err)
	}
	defer sr.Close()

	// Pass 1: reads from the source and spools.
	if err := sr.Rewind(); err != nil {
		t.Fatalf("rewind pass1: %v", err)
	}
	first, err := io.ReadAll(sr)
	if err != nil {
		t.Fatalf("read pass1: %v", err)
	}
	if !bytes.Equal(first, []byte("hello teldrive part bytes")) {
		t.Fatalf("pass1 mismatch: %q", first)
	}

	// Pass 2: a re-auth retry must replay the exact same bytes from the spool,
	// even though the original source is now exhausted.
	if err := sr.Rewind(); err != nil {
		t.Fatalf("rewind pass2: %v", err)
	}
	second, err := io.ReadAll(sr)
	if err != nil {
		t.Fatalf("read pass2: %v", err)
	}
	if !bytes.Equal(second, first) {
		t.Fatalf("pass2 mismatch: %q != %q", second, first)
	}
}

func TestSpooledReaderReplaysManyPasses(t *testing.T) {
	src := bytes.NewReader([]byte("0123456789"))
	sr, err := newSpooledReader(src)
	if err != nil {
		t.Fatalf("newSpooledReader: %v", err)
	}
	defer sr.Close()

	var first []byte
	for pass := 1; pass <= 3; pass++ {
		if err := sr.Rewind(); err != nil {
			t.Fatalf("rewind pass %d: %v", pass, err)
		}
		got, err := io.ReadAll(sr)
		if err != nil {
			t.Fatalf("read pass %d: %v", pass, err)
		}
		if pass == 1 {
			first = got
			continue
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("pass %d mismatch: %q != %q", pass, got, first)
		}
	}
}
