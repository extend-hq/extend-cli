package extendx

import (
	"testing"

	extend "github.com/extend-hq/extend-go-sdk"
)

func TestVersionPtr(t *testing.T) {
	if VersionPtr("") != nil {
		t.Error("VersionPtr(\"\") must return nil so absent serializes as omitted")
	}
	got := VersionPtr("v3")
	if got == nil {
		t.Fatal("VersionPtr(v3) = nil; want non-nil")
	}
	if *got != extend.ProcessorVersionString("v3") {
		t.Errorf("VersionPtr(v3) = %q; want v3", *got)
	}
}

func TestPriorityPtr(t *testing.T) {
	// 0 and negative must return nil so the wire body omits priority
	// and the server applies its default.
	if PriorityPtr(0) != nil {
		t.Error("PriorityPtr(0) must return nil")
	}
	if PriorityPtr(-1) != nil {
		t.Error("PriorityPtr(-1) must return nil")
	}
	got := PriorityPtr(7)
	if got == nil || *got != extend.RunPriority(7) {
		t.Errorf("PriorityPtr(7) = %v; want pointer to 7", got)
	}
}

func TestMetadataPtr(t *testing.T) {
	// nil map → nil pointer so absent serializes as omitted.
	if MetadataPtr(nil) != nil {
		t.Error("MetadataPtr(nil) must return nil")
	}
	// Empty (non-nil) map round-trips as an empty *RunMetadata so
	// callers can distinguish "absent" from "explicitly cleared".
	empty := MetadataPtr(map[string]any{})
	if empty == nil {
		t.Fatal("MetadataPtr(empty map) = nil; expected non-nil")
	}
	if len(*empty) != 0 {
		t.Errorf("MetadataPtr(empty) = %v; want empty map", *empty)
	}

	md := MetadataPtr(map[string]any{"k": "v"})
	if md == nil {
		t.Fatal("MetadataPtr(populated) = nil")
	}
	if (*md)["k"] != "v" {
		t.Errorf("MetadataPtr.k = %v; want v", (*md)["k"])
	}
}

func TestDeref(t *testing.T) {
	// nil → zero value of T.
	var nilStr *string
	if got := Deref(nilStr); got != "" {
		t.Errorf("Deref(nil *string) = %q; want \"\"", got)
	}
	var nilInt *int
	if got := Deref(nilInt); got != 0 {
		t.Errorf("Deref(nil *int) = %d; want 0", got)
	}

	// Populated pointer returns its value.
	s := "hello"
	if got := Deref(&s); got != "hello" {
		t.Errorf("Deref(&\"hello\") = %q; want hello", got)
	}
	n := 42
	if got := Deref(&n); got != 42 {
		t.Errorf("Deref(&42) = %d; want 42", got)
	}
}
