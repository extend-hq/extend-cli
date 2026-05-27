package cli

import (
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
)

func TestVersionPtr(t *testing.T) {
	if versionPtr("") != nil {
		t.Error("versionPtr(\"\") must return nil so absent serializes as omitted")
	}
	got := versionPtr("v3")
	if got == nil {
		t.Fatal("versionPtr(v3) = nil; want non-nil")
	}
	if *got != extend.ProcessorVersionString("v3") {
		t.Errorf("versionPtr(v3) = %q; want v3", *got)
	}
}

func TestPriorityPtr(t *testing.T) {
	// 0 and negative must return nil so the wire body omits priority
	// and the server applies its default.
	if priorityPtr(0) != nil {
		t.Error("priorityPtr(0) must return nil")
	}
	if priorityPtr(-1) != nil {
		t.Error("priorityPtr(-1) must return nil")
	}
	got := priorityPtr(7)
	if got == nil || *got != extend.RunPriority(7) {
		t.Errorf("priorityPtr(7) = %v; want pointer to 7", got)
	}
}

func TestMetadataPtr(t *testing.T) {
	// nil map → nil pointer so absent serializes as omitted.
	if metadataPtr(nil) != nil {
		t.Error("metadataPtr(nil) must return nil")
	}
	// Empty (non-nil) map round-trips as an empty *RunMetadata so
	// callers can distinguish "absent" from "explicitly cleared".
	empty := metadataPtr(map[string]any{})
	if empty == nil {
		t.Fatal("metadataPtr(empty map) = nil; expected non-nil")
	}
	if len(*empty) != 0 {
		t.Errorf("metadataPtr(empty) = %v; want empty map", *empty)
	}

	md := metadataPtr(map[string]any{"k": "v"})
	if md == nil {
		t.Fatal("metadataPtr(populated) = nil")
	}
	if (*md)["k"] != "v" {
		t.Errorf("metadataPtr.k = %v; want v", (*md)["k"])
	}
}

func TestDeref(t *testing.T) {
	// nil → zero value of T.
	var nilStr *string
	if got := deref(nilStr); got != "" {
		t.Errorf("deref(nil *string) = %q; want \"\"", got)
	}
	var nilInt *int
	if got := deref(nilInt); got != 0 {
		t.Errorf("deref(nil *int) = %d; want 0", got)
	}

	// Populated pointer returns its value.
	s := "hello"
	if got := deref(&s); got != "hello" {
		t.Errorf("deref(&\"hello\") = %q; want hello", got)
	}
	n := 42
	if got := deref(&n); got != 42 {
		t.Errorf("deref(&42) = %d; want 42", got)
	}
}
