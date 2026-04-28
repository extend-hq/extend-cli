package cli

import "os"

func writeFileForTest(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
