package extendx

import extend "github.com/extend-hq/extend-go-sdk"

// VersionPtr returns a *ProcessorVersionString for the given non-empty
// version string, or nil if empty. The SDK's processor-reference types
// (e.g. *extend.ExtractRunsCreateRequestExtractor.Version) take this
// as a pointer so absent and explicit-empty serialize differently;
// the CLI never wants to send empty so we treat "" as absent.
func VersionPtr(v string) *extend.ProcessorVersionString {
	if v == "" {
		return nil
	}
	pv := extend.ProcessorVersionString(v)
	return &pv
}

// PriorityPtr returns a *RunPriority for any positive priority value.
// 0 (the flag default for --priority) maps to nil so the on-the-wire
// body omits the field entirely and the server applies its default.
func PriorityPtr(p int) *extend.RunPriority {
	if p <= 0 {
		return nil
	}
	pr := extend.RunPriority(p)
	return &pr
}

// MetadataPtr returns a *RunMetadata for the given map, or nil if the
// map is nil. The SDK's request types take metadata as a pointer-to-map
// so explicit-nil and absent both serialize cleanly; we only ever send
// "absent" (no metadata flag set) so the nil → nil mapping is safe.
func MetadataPtr(md map[string]any) *extend.RunMetadata {
	if md == nil {
		return nil
	}
	mm := extend.RunMetadata(md)
	return &mm
}

// Deref safely dereferences a pointer, returning the zero value of T
// when the pointer is nil. Used in render paths where SDK responses
// expose optional fields as *T and the caller wants the value-or-zero
// without a nil guard at every site.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
