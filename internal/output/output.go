// Package output renders API payloads in user-selected formats (json, yaml, raw, id)
// with optional --jq filtering. Table rendering is intentionally separate so callers
// can supply type-specific columns; see RenderTable.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

type Format string

const (
	FormatJSON     Format = "json"
	FormatYAML     Format = "yaml"
	FormatRaw      Format = "raw"
	FormatID       Format = "id"
	FormatTable    Format = "table"
	FormatMarkdown Format = "markdown"
)

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "", "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	case "raw":
		return FormatRaw, nil
	case "id":
		return FormatID, nil
	case "table":
		return FormatTable, nil
	case "markdown", "md":
		return FormatMarkdown, nil
	default:
		return "", fmt.Errorf("unknown output format %q (want one of: json, yaml, raw, id, table, markdown)", s)
	}
}

type Options struct {
	JQ     string
	Pretty bool
}

type Option func(*Options)

func WithJQ(expr string) Option { return func(o *Options) { o.JQ = expr } }
func WithPretty(p bool) Option  { return func(o *Options) { o.Pretty = p } }

// Render formats payload and writes it to w. FormatTable is rejected here because
// table columns depend on the concrete type; use RenderTable instead.
func Render(w io.Writer, format Format, payload any, opts ...Option) error {
	o := Options{Pretty: true}
	for _, opt := range opts {
		opt(&o)
	}

	if format == FormatTable {
		return fmt.Errorf("table format requires RenderTable, not Render")
	}

	value, err := normalize(payload)
	if err != nil {
		return err
	}

	if o.JQ != "" {
		filtered, err := applyJQ(o.JQ, value)
		if err != nil {
			return err
		}
		return renderValues(w, format, filtered, o)
	}

	return renderValues(w, format, []any{value}, o)
}

func renderValues(w io.Writer, format Format, values []any, o Options) error {
	switch format {
	case FormatJSON:
		return writeJSON(w, values, o.Pretty)
	case FormatYAML:
		return writeYAML(w, values)
	case FormatRaw:
		return writeRaw(w, values)
	case FormatID:
		return writeIDs(w, values)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func writeJSON(w io.Writer, values []any, pretty bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

func writeYAML(w io.Writer, values []any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return enc.Close()
}

func writeRaw(w io.Writer, values []any) error {
	for _, v := range values {
		switch t := v.(type) {
		case string:
			if _, err := fmt.Fprintln(w, t); err != nil {
				return err
			}
		case nil:
		default:
			if err := writeJSON(w, []any{v}, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeIDs(w io.Writer, values []any) error {
	for _, v := range values {
		id, ok := extractID(v)
		if !ok {
			return fmt.Errorf("--output id requires payload with an 'id' field; got %T", v)
		}
		if _, err := fmt.Fprintln(w, id); err != nil {
			return err
		}
	}
	return nil
}

func extractID(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	if m, ok := v.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return id, true
		}
	}
	return "", false
}

// normalize turns an arbitrary Go value into a JSON-compatible structure
// (map[string]any / []any / scalar) by round-tripping through encoding/json.
// This keeps downstream rendering uniform regardless of the source type.
func normalize(payload any) (any, error) {
	if payload == nil {
		return nil, nil
	}
	switch v := payload.(type) {
	case map[string]any, []any, string, float64, bool:
		return v, nil
	case json.RawMessage:
		var out any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, fmt.Errorf("invalid json.RawMessage: %w", err)
		}
		return out, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("normalize payload: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("re-decode payload: %w", err)
	}
	return out, nil
}

func applyJQ(expr string, value any) ([]any, error) {
	q, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("parse --jq: %w", err)
	}
	iter := q.Run(value)
	var out []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("jq runtime: %w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

func RenderTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	upper := make([]string, len(headers))
	for i, h := range headers {
		upper[i] = strings.ToUpper(h)
	}
	if _, err := fmt.Fprintln(tw, strings.Join(upper, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// RenderMarkdownTable writes a GitHub-flavored pipe table: a header row,
// a `--- | ---` separator, then one row per record. Headers are uppercased
// to match RenderTable's tabwriter output. Cells are written verbatim;
// callers should pre-escape any literal `|` characters.
func RenderMarkdownTable(w io.Writer, headers []string, rows [][]string) error {
	if len(headers) == 0 {
		return nil
	}
	upper := make([]string, len(headers))
	for i, h := range headers {
		upper[i] = strings.ToUpper(h)
	}
	if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(upper, " | ")); err != nil {
		return err
	}
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = "---"
	}
	if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(sep, " | ")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(row, " | ")); err != nil {
			return err
		}
	}
	return nil
}
