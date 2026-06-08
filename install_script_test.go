package extendcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallScriptInstallsFromReleaseArchive(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh targets Unix-like platforms")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required to run install.sh")
	}

	tmp := t.TempDir()
	releaseDir := filepath.Join(tmp, "release")
	officialDir := filepath.Join(tmp, "official")
	binDir := filepath.Join(tmp, "bin")
	fakePath := filepath.Join(tmp, "fakebin")
	setupLog := filepath.Join(tmp, "setup.log")
	mustMkdirAll(t, releaseDir)
	mustMkdirAll(t, officialDir)
	mustMkdirAll(t, binDir)
	mustMkdirAll(t, fakePath)

	archiveName := fmt.Sprintf("extend_v9.9.9_%s_%s.tar.gz", runtime.GOOS, goArchForInstallTest(t))
	archivePath := filepath.Join(releaseDir, archiveName)
	writeReleaseArchive(t, archivePath, []byte(fakeExtendBinary()))
	writeChecksums(t, releaseDir, archiveName, archivePath, true)
	writeChecksums(t, officialDir, archiveName, archivePath, false)
	writeFakeCurl(t, fakePath)

	cmd := exec.Command("sh", "install.sh", "--version", "v9.9.9", "--bin-dir", binDir)
	cmd.Env = append(os.Environ(),
		"EXTEND_RELEASE_BASE_URL=file://"+releaseDir,
		"EXTEND_FAKE_SETUP_LOG="+setupLog,
		"FAKE_OFFICIAL_RELEASE_DIR="+officialDir,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}

	installed := filepath.Join(binDir, "extend")
	if info, err := os.Stat(installed); err != nil {
		t.Fatalf("installed binary missing: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %s", info.Mode())
	}

	versionOut, err := exec.Command(installed, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("installed binary failed: %v\n%s", err, versionOut)
	}
	if got, want := strings.TrimSpace(string(versionOut)), "extend test v9.9.9"; got != want {
		t.Fatalf("installed binary output = %q, want %q", got, want)
	}
	if got := readFileString(t, setupLog); strings.TrimSpace(got) != "setup skip=0" {
		t.Fatalf("setup log = %q, want \"setup skip=0\" (delegated to setup, skip knob forwarded)", got)
	}
}

// TestInstallScriptForwardsSkipSkill confirms --skip-skill-install reaches
// the CLI as EXTEND_SKIP_SKILL_INSTALL=1 (the CLI, not the script, now owns
// suppressing the skill).
func TestInstallScriptForwardsSkipSkill(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh targets Unix-like platforms")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required to run install.sh")
	}

	tmp := t.TempDir()
	releaseDir := filepath.Join(tmp, "release")
	officialDir := filepath.Join(tmp, "official")
	binDir := filepath.Join(tmp, "bin")
	fakePath := filepath.Join(tmp, "fakebin")
	setupLog := filepath.Join(tmp, "setup.log")
	mustMkdirAll(t, releaseDir)
	mustMkdirAll(t, officialDir)
	mustMkdirAll(t, binDir)
	mustMkdirAll(t, fakePath)

	archiveName := fmt.Sprintf("extend_v9.9.9_%s_%s.tar.gz", runtime.GOOS, goArchForInstallTest(t))
	archivePath := filepath.Join(releaseDir, archiveName)
	writeReleaseArchive(t, archivePath, []byte(fakeExtendBinary()))
	writeChecksums(t, releaseDir, archiveName, archivePath, true)
	writeChecksums(t, officialDir, archiveName, archivePath, false)
	writeFakeCurl(t, fakePath)

	cmd := exec.Command("sh", "install.sh", "--version", "v9.9.9", "--bin-dir", binDir, "--skip-skill-install")
	cmd.Env = append(os.Environ(),
		"EXTEND_RELEASE_BASE_URL=file://"+releaseDir,
		"EXTEND_FAKE_SETUP_LOG="+setupLog,
		"FAKE_OFFICIAL_RELEASE_DIR="+officialDir,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	if got := readFileString(t, setupLog); strings.TrimSpace(got) != "setup skip=1" {
		t.Fatalf("setup log = %q, want \"setup skip=1\"", got)
	}
}

func TestInstallScriptRejectsChecksumMismatch(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh targets Unix-like platforms")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required to run install.sh")
	}

	tmp := t.TempDir()
	releaseDir := filepath.Join(tmp, "release")
	officialDir := filepath.Join(tmp, "official")
	binDir := filepath.Join(tmp, "bin")
	fakePath := filepath.Join(tmp, "fakebin")
	setupLog := filepath.Join(tmp, "setup.log")
	mustMkdirAll(t, releaseDir)
	mustMkdirAll(t, officialDir)
	mustMkdirAll(t, binDir)
	mustMkdirAll(t, fakePath)

	archiveName := fmt.Sprintf("extend_v9.9.9_%s_%s.tar.gz", runtime.GOOS, goArchForInstallTest(t))
	archivePath := filepath.Join(releaseDir, archiveName)
	writeReleaseArchive(t, archivePath, []byte(fakeExtendBinary()))
	writeChecksums(t, releaseDir, archiveName, archivePath, false)
	writeChecksums(t, officialDir, archiveName, archivePath, true)
	writeFakeCurl(t, fakePath)

	cmd := exec.Command("sh", "install.sh", "--version", "v9.9.9", "--bin-dir", binDir)
	cmd.Env = append(os.Environ(),
		"EXTEND_RELEASE_BASE_URL=file://"+releaseDir,
		"EXTEND_FAKE_SETUP_LOG="+setupLog,
		"FAKE_OFFICIAL_RELEASE_DIR="+officialDir,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh succeeded with a bad checksum\n%s", out)
	}
	if !strings.Contains(string(out), "checksum mismatch") {
		t.Fatalf("install.sh output = %q, want checksum mismatch", out)
	}
	if _, err := os.Stat(filepath.Join(binDir, "extend")); !os.IsNotExist(err) {
		t.Fatalf("binary should not be installed after checksum mismatch, stat err: %v", err)
	}
	if _, err := os.Stat(setupLog); !os.IsNotExist(err) {
		t.Fatalf("setup should not run after checksum mismatch, stat err: %v", err)
	}
}

func goArchForInstallTest(t *testing.T) string {
	t.Helper()
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	default:
		t.Skipf("install.sh does not support %s", runtime.GOARCH)
		return ""
	}
}

func fakeExtendBinary() string {
	return `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' 'extend test v9.9.9'
  exit 0
fi
if [ "${1:-}" = "setup" ]; then
  printf '%s skip=%s\n' "$*" "${EXTEND_SKIP_SKILL_INSTALL-unset}" >> "${EXTEND_FAKE_SETUP_LOG:?}"
  exit 0
fi
printf '%s\n' 'extend test'
`
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func writeReleaseArchive(t *testing.T, path string, binary []byte) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "extend", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write release archive: %v", err)
	}
}

func writeChecksums(t *testing.T, releaseDir, archiveName, archivePath string, corrupt bool) {
	t.Helper()

	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read release archive: %v", err)
	}
	sum := sha256.Sum256(archive)
	checksum := fmt.Sprintf("%x", sum)
	if corrupt {
		checksum = strings.Repeat("0", len(checksum))
	}
	contents := fmt.Sprintf("%s  %s\n", checksum, archiveName)
	if err := os.WriteFile(filepath.Join(releaseDir, "SHA256SUMS"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write SHA256SUMS: %v", err)
	}
}

func writeFakeCurl(t *testing.T, dir string) {
	t.Helper()

	contents := `#!/bin/sh
set -eu
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
case "$url" in
  */SHA256SUMS) path=${FAKE_OFFICIAL_RELEASE_DIR:?}/SHA256SUMS ;;
  file://*) path=${url#file://} ;;
  *) echo "unexpected URL: $url" >&2; exit 2 ;;
esac
if [ -n "$out" ]; then
  cp "$path" "$out"
else
  cat "$path"
fi
`
	path := filepath.Join(dir, "curl")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write fake curl: %v", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
