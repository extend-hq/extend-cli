// Package extendx contains CLI-only abstractions the SDK doesn't (and
// shouldn't) provide:
//
//   - Short region selector (us|us2|eu) → SDK Environment URLs
//   - File-input resolver covering local-path / stdin / file_id / URL
//     plus the 12 per-endpoint union builders the SDK exposes
//   - Polling helper PollForRun with per-resource wait profiles
//   - Webhook signature verification for *incoming* HTTP requests
//   - Debug HTTP transport for --debug logging
//   - ID-prefix dispatchers (RunKindFromID, BatchKindFromID, ...) that
//     map opaque IDs to the correct SDK sub-client
//   - Collapsed RunStatus enum unifying the SDK's per-kind status types
//   - APIError extractor that flattens the SDK's four error wrapper
//     types into a single shape the CLI's error printer can consume
//   - NewClient factory that converts the CLI's env-resolved Config
//     into the SDK option.With* calls
//
// Code in this package never calls the Extend API directly; every
// transport-level concern is delegated to the SDK. Helpers here exist
// because they are CLI-specific (env vars resolved by the parent
// `cli` package; ID-prefix routing; polling cadence) and have no
// equivalent in the SDK.
package extendx
