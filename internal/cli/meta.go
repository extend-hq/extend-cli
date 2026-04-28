package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const usageTagsKey = "extend:usage_tags"

type metaFlags struct {
	metadata []string
	tags     []string
}

func (m *metaFlags) attach(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&m.metadata, "metadata", nil, "key=value metadata (repeatable)")
	cmd.Flags().StringArrayVar(&m.tags, "tag", nil, "Usage tag(s); repeatable or comma-separated")
}

func (m *metaFlags) build() (map[string]any, error) {
	out := map[string]any{}
	pairs, err := parseKVPairs("--metadata", m.metadata)
	if err != nil {
		return nil, err
	}
	for k, v := range pairs {
		out[k] = v
	}
	if tags := splitCSV(m.tags); len(tags) > 0 {
		out[usageTagsKey] = tags
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// parseKVPairs splits a slice of "key=value" strings (typically from a
// repeatable string-array flag) into a map. The flag name is used to make
// error messages actionable. Empty keys (entries starting with "=") and
// missing separators are rejected.
func parseKVPairs(flag string, pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid %s %q (want key=value)", flag, kv)
		}
		out[kv[:idx]] = kv[idx+1:]
	}
	return out, nil
}
