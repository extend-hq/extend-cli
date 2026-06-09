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
	if got := readFileString(t, setupLog); strings.TrimSpace(got) != "setup" {
		t.Fatalf("setup log = %q, want \"setup\" (delegated to setup with no skip flag)", got)
	}
}

// TestInstallScriptForwardsSkipSkill confirms --skip-skill-install is
// forwarded to the CLI as the same --skip-skill-install flag (the CLI,
// not the script, owns suppressing the skill).
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
	if got := readFileString(t, setupLog); strings.TrimSpace(got) != "setup --skip-skill-install" {
		t.Fatalf("setup log = %q, want \"setup --skip-skill-install\"", got)
	}
}

// TestInstallScriptWarnsWhenShadowed: the install dir is on PATH, but a
// different `extend` sits ahead of it. The installer must warn loudly and
// name both binaries — this is the exact trap where the wizard (run by
// absolute path) configures the new binary while the user's shell keeps
// running the old one, so saved credentials look "missing".
func TestInstallScriptWarnsWhenShadowed(t *testing.T) {
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
	shadowDir := filepath.Join(tmp, "shadow")
	setupLog := filepath.Join(tmp, "setup.log")
	mustMkdirAll(t, releaseDir)
	mustMkdirAll(t, officialDir)
	mustMkdirAll(t, binDir)
	mustMkdirAll(t, fakePath)
	mustMkdirAll(t, shadowDir)

	archiveName := fmt.Sprintf("extend_v9.9.9_%s_%s.tar.gz", runtime.GOOS, goArchForInstallTest(t))
	archivePath := filepath.Join(releaseDir, archiveName)
	writeReleaseArchive(t, archivePath, []byte(fakeExtendBinary()))
	writeChecksums(t, releaseDir, archiveName, archivePath, true)
	writeChecksums(t, officialDir, archiveName, archivePath, false)
	writeFakeCurl(t, fakePath)
	// A pre-existing `extend` ahead of the install dir on PATH.
	shadowExtend := filepath.Join(shadowDir, "extend")
	if err := os.WriteFile(shadowExtend, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write shadow extend: %v", err)
	}

	sep := string(os.PathListSeparator)
	cmd := exec.Command("sh", "install.sh", "--version", "v9.9.9", "--bin-dir", binDir)
	cmd.Env = append(os.Environ(),
		"EXTEND_RELEASE_BASE_URL=file://"+releaseDir,
		"EXTEND_FAKE_SETUP_LOG="+setupLog,
		"FAKE_OFFICIAL_RELEASE_DIR="+officialDir,
		// shadowDir is ahead of binDir; both are on PATH.
		"PATH="+fakePath+sep+shadowDir+sep+binDir+sep+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}

	target := filepath.Join(binDir, "extend")
	for _, want := range []string{"shadows", shadowExtend, target, "hash -r"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("install.sh output missing %q; got:\n%s", want, out)
		}
	}
}

// TestInstallScriptNoShadowWarningWhenFirstOnPath: when the install dir is
// first on PATH (so `extend` resolves to the freshly installed binary), the
// installer must NOT emit a shadow warning.
func TestInstallScriptNoShadowWarningWhenFirstOnPath(t *testing.T) {
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

	sep := string(os.PathListSeparator)
	cmd := exec.Command("sh", "install.sh", "--version", "v9.9.9", "--bin-dir", binDir)
	cmd.Env = append(os.Environ(),
		"EXTEND_RELEASE_BASE_URL=file://"+releaseDir,
		"EXTEND_FAKE_SETUP_LOG="+setupLog,
		"FAKE_OFFICIAL_RELEASE_DIR="+officialDir,
		// binDir is first, so the freshly installed binary wins.
		"PATH="+binDir+sep+fakePath+sep+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "shadows") {
		t.Fatalf("install.sh warned about shadowing when install dir is first on PATH; got:\n%s", out)
	}
	if got := readFileString(t, setupLog); strings.TrimSpace(got) != "setup" {
		t.Fatalf("setup log = %q, want \"setup\"", got)
	}
}

// installFixture is the scaffolding shared by the install-dir detection
// tests: a release archive + checksums, a fake curl, and a fake HOME. The
// returned env deliberately uses a minimal hermetic PATH (system tool dirs
// only, no inherited PATH) so `command -v extend` inside the script can
// never see a real extend on the developer's machine — the upgrade-in-place
// branch would happily overwrite it.
type installFixture struct {
	tmp      string
	home     string
	fakePath string
	setupLog string
	baseEnv  []string
}

func newInstallFixture(t *testing.T) *installFixture {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh targets Unix-like platforms")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required to run install.sh")
	}

	tmp := t.TempDir()
	releaseDir := filepath.Join(tmp, "release")
	officialDir := filepath.Join(tmp, "official")
	fakePath := filepath.Join(tmp, "fakebin")
	home := filepath.Join(tmp, "home")
	setupLog := filepath.Join(tmp, "setup.log")
	mustMkdirAll(t, releaseDir)
	mustMkdirAll(t, officialDir)
	mustMkdirAll(t, fakePath)
	mustMkdirAll(t, home)

	archiveName := fmt.Sprintf("extend_v9.9.9_%s_%s.tar.gz", runtime.GOOS, goArchForInstallTest(t))
	archivePath := filepath.Join(releaseDir, archiveName)
	writeReleaseArchive(t, archivePath, []byte(fakeExtendBinary()))
	writeChecksums(t, releaseDir, archiveName, archivePath, true)
	writeChecksums(t, officialDir, archiveName, archivePath, false)
	writeFakeCurl(t, fakePath)

	return &installFixture{
		tmp:      tmp,
		home:     home,
		fakePath: fakePath,
		setupLog: setupLog,
		baseEnv: []string{
			"EXTEND_RELEASE_BASE_URL=file://" + releaseDir,
			"EXTEND_FAKE_SETUP_LOG=" + setupLog,
			"FAKE_OFFICIAL_RELEASE_DIR=" + officialDir,
			"HOME=" + home,
		},
	}
}

// run executes install.sh with the fixture env, extra script args, the
// given PATH dirs (fake curl first, system tool dirs last), and extra env
// entries.
func (f *installFixture) run(t *testing.T, args []string, pathDirs []string, extraEnv ...string) string {
	t.Helper()
	sep := string(os.PathListSeparator)
	path := f.fakePath
	for _, d := range pathDirs {
		path += sep + d
	}
	path += sep + "/usr/bin" + sep + "/bin" + sep + "/usr/sbin" + sep + "/sbin"

	cmd := exec.Command("sh", append([]string{"install.sh", "--version", "v9.9.9"}, args...)...)
	cmd.Env = append(append([]string{}, f.baseEnv...), extraEnv...)
	cmd.Env = append(cmd.Env, "PATH="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestInstallScriptDetectsCandidateOnPath: with no --bin-dir, the installer
// picks the first candidate dir that is both on PATH and writable, instead
// of blindly defaulting to ~/.local/bin (which is not on PATH on a default
// macOS shell). Candidates are injected so the test never touches real
// system dirs.
func TestInstallScriptDetectsCandidateOnPath(t *testing.T) {
	f := newInstallFixture(t)

	offPath := filepath.Join(f.home, ".local", "bin") // candidate 1: not on PATH
	onPath := filepath.Join(f.tmp, "onpath")          // candidate 2: on PATH, writable
	mustMkdirAll(t, onPath)

	out := f.run(t, nil, []string{onPath},
		"EXTEND_INSTALL_CANDIDATES="+offPath+":"+onPath)

	if _, err := os.Stat(filepath.Join(onPath, "extend")); err != nil {
		t.Fatalf("binary not installed into the on-PATH candidate: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(offPath, "extend")); !os.IsNotExist(err) {
		t.Fatalf("binary should not land in the off-PATH candidate, stat err: %v\n%s", err, out)
	}
	if strings.Contains(out, "is not on PATH") {
		t.Fatalf("no PATH warning expected when an on-PATH dir was chosen; got:\n%s", out)
	}
}

// TestInstallScriptUpgradesInPlace: an existing extend (regular file, in a
// writable dir on PATH) is replaced where it lives, even when that dir is
// not in the candidate list. This is the rule that prevents a second copy
// shadow-fighting an old install.
func TestInstallScriptUpgradesInPlace(t *testing.T) {
	f := newInstallFixture(t)

	oldDir := filepath.Join(f.tmp, "oldbin")
	mustMkdirAll(t, oldDir)
	oldExtend := filepath.Join(oldDir, "extend")
	if err := os.WriteFile(oldExtend, []byte("#!/bin/sh\necho extend OLD\n"), 0o755); err != nil {
		t.Fatalf("write old extend: %v", err)
	}

	// Candidates exclude oldDir to prove in-place wins over the candidate scan.
	out := f.run(t, nil, []string{oldDir},
		"EXTEND_INSTALL_CANDIDATES="+filepath.Join(f.home, ".local", "bin"))

	versionOut, err := exec.Command(oldExtend, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("upgraded binary failed: %v\n%s", err, versionOut)
	}
	if got, want := strings.TrimSpace(string(versionOut)), "extend test v9.9.9"; got != want {
		t.Fatalf("binary at old location = %q, want %q (not upgraded in place)\n%s", got, want, out)
	}
	if strings.Contains(out, "shadows") {
		t.Fatalf("in-place upgrade must not trigger the shadow warning; got:\n%s", out)
	}
}

// TestInstallScriptSkipsManagedSymlinkDir: a candidate dir whose `extend`
// is a symlink (a package manager's, e.g. Homebrew) is skipped — both by
// upgrade-in-place and by the candidate scan — and the next usable
// candidate wins. The shadow warning then points at the symlinked one.
func TestInstallScriptSkipsManagedSymlinkDir(t *testing.T) {
	f := newInstallFixture(t)

	brewLike := filepath.Join(f.tmp, "brewbin")
	mustMkdirAll(t, brewLike)
	cellar := filepath.Join(f.tmp, "cellar-extend")
	if err := os.WriteFile(cellar, []byte("#!/bin/sh\necho brew extend\n"), 0o755); err != nil {
		t.Fatalf("write cellar binary: %v", err)
	}
	brewExtend := filepath.Join(brewLike, "extend")
	if err := os.Symlink(cellar, brewExtend); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	nextDir := filepath.Join(f.tmp, "nextbin")
	mustMkdirAll(t, nextDir)

	// brewLike is earlier on PATH than nextDir, and first in candidates.
	out := f.run(t, nil, []string{brewLike, nextDir},
		"EXTEND_INSTALL_CANDIDATES="+brewLike+":"+nextDir)

	if fi, err := os.Lstat(brewExtend); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("managed symlink must be left untouched (err=%v, mode=%v)\n%s", err, fi.Mode(), out)
	}
	if _, err := os.Stat(filepath.Join(nextDir, "extend")); err != nil {
		t.Fatalf("binary not installed into next candidate: %v\n%s", err, out)
	}
	for _, want := range []string{"shadows", brewExtend} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q (shadow warning should name the symlinked extend); got:\n%s", want, out)
		}
	}
}

// TestInstallScriptFallsBackWhenNoCandidateOnPath: nothing usable on PATH
// and profile modification opted out → the historical default (~/.local/bin)
// plus the not-on-PATH warning, and no profile file is touched.
func TestInstallScriptFallsBackWhenNoCandidateOnPath(t *testing.T) {
	f := newInstallFixture(t)

	out := f.run(t, []string{"--no-modify-path"}, nil,
		"SHELL=/bin/zsh",
		"EXTEND_INSTALL_CANDIDATES="+filepath.Join(f.home, ".local", "bin"))

	if _, err := os.Stat(filepath.Join(f.home, ".local", "bin", "extend")); err != nil {
		t.Fatalf("binary not installed into fallback ~/.local/bin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "is not on PATH") {
		t.Fatalf("expected the not-on-PATH warning; got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zprofile")); !os.IsNotExist(err) {
		t.Fatalf("--no-modify-path must not write a profile, stat err: %v\n%s", err, out)
	}
}

// TestInstallScriptAddsFallbackDirToZshProfile: the stock-Mac case — zsh,
// nothing usable on PATH → install to ~/.local/bin and append one guarded
// PATH line to ~/.zprofile (macOS terminals are login shells). Running the
// installer again must not duplicate the line.
func TestInstallScriptAddsFallbackDirToZshProfile(t *testing.T) {
	f := newInstallFixture(t)
	env := []string{
		"SHELL=/bin/zsh",
		"EXTEND_INSTALL_CANDIDATES=" + filepath.Join(f.home, ".local", "bin"),
	}
	// Pre-existing profile WITHOUT a trailing newline: the append must not
	// concatenate onto the user's last line.
	profile := filepath.Join(f.home, ".zprofile")
	if err := os.WriteFile(profile, []byte("# theirs"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := f.run(t, nil, nil, env...)

	wantLine := `export PATH="$HOME/.local/bin:$PATH"`
	got := readFileString(t, profile)
	if !strings.Contains(got, wantLine) {
		t.Fatalf(".zprofile missing %q; got:\n%s\ninstaller output:\n%s", wantLine, got, out)
	}
	if !strings.Contains(got, "# theirs\n") {
		t.Fatalf("append corrupted the user's unterminated last line; got:\n%s", got)
	}
	if !strings.Contains(out, ".zprofile") {
		t.Fatalf("installer should say which profile it modified; got:\n%s", out)
	}
	if strings.Contains(out, "is not on PATH") {
		t.Fatalf("no warning expected when the profile was fixed; got:\n%s", out)
	}

	// Idempotent: a second run appends nothing.
	f.run(t, nil, nil, env...)
	if got := readFileString(t, profile); strings.Count(got, wantLine) != 1 {
		t.Fatalf("PATH line duplicated after second run:\n%s", got)
	}
}

// TestInstallScriptPicksBashProfileFile: bash login shells read the first
// existing of .bash_profile, .bash_login, .profile — and skip .profile when
// .bash_profile exists, so the line must land in the file bash will read.
func TestInstallScriptPicksBashProfileFile(t *testing.T) {
	f := newInstallFixture(t)
	bashProfile := filepath.Join(f.home, ".bash_profile")
	if err := os.WriteFile(bashProfile, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.run(t, nil, nil,
		"SHELL=/bin/bash",
		"EXTEND_INSTALL_CANDIDATES="+filepath.Join(f.home, ".local", "bin"))

	if got := readFileString(t, bashProfile); !strings.Contains(got, `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf(".bash_profile missing the PATH line; got:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".profile")); !os.IsNotExist(err) {
		t.Fatalf(".profile must not be written when .bash_profile exists, stat err: %v", err)
	}
}

// TestInstallScriptWritesFishConfD: fish never reads POSIX profiles, so the
// PATH fix is a conf.d snippet using fish_add_path.
func TestInstallScriptWritesFishConfD(t *testing.T) {
	f := newInstallFixture(t)

	f.run(t, nil, nil,
		"SHELL=/opt/homebrew/bin/fish",
		"EXTEND_INSTALL_CANDIDATES="+filepath.Join(f.home, ".local", "bin"))

	snippet := filepath.Join(f.home, ".config", "fish", "conf.d", "extend.fish")
	if got := readFileString(t, snippet); !strings.Contains(got, `fish_add_path "$HOME/.local/bin"`) {
		t.Fatalf("fish conf.d snippet missing fish_add_path; got:\n%s", got)
	}
}

// TestInstallScriptDefaultCandidates: without EXTEND_INSTALL_CANDIDATES the
// shipped default list applies — ~/.local/bin is picked when on PATH. Every
// other detection test injects candidates, so this is the only pin on the
// real default string; a typo there would pass the rest of the suite.
func TestInstallScriptDefaultCandidates(t *testing.T) {
	f := newInstallFixture(t)
	localBin := filepath.Join(f.home, ".local", "bin")
	mustMkdirAll(t, localBin)

	out := f.run(t, nil, []string{localBin}, "SHELL=/bin/zsh")

	if _, err := os.Stat(filepath.Join(localBin, "extend")); err != nil {
		t.Fatalf("binary not installed into default candidate ~/.local/bin: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zprofile")); !os.IsNotExist(err) {
		t.Fatalf("no profile edit expected when a default candidate is on PATH, stat err: %v\n%s", err, out)
	}
}

// TestInstallScriptHonorsZdotdir: zsh users relocate their dotfiles with
// ZDOTDIR; the PATH line must land in $ZDOTDIR/.zprofile, not $HOME.
func TestInstallScriptHonorsZdotdir(t *testing.T) {
	f := newInstallFixture(t)
	zdot := filepath.Join(f.tmp, "zdot")
	mustMkdirAll(t, zdot)

	out := f.run(t, nil, nil,
		"SHELL=/bin/zsh",
		"ZDOTDIR="+zdot,
		"EXTEND_INSTALL_CANDIDATES="+filepath.Join(f.home, ".local", "bin"))

	if got := readFileString(t, filepath.Join(zdot, ".zprofile")); !strings.Contains(got, `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf("$ZDOTDIR/.zprofile missing the PATH line; got:\n%s\noutput:\n%s", got, out)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zprofile")); !os.IsNotExist(err) {
		t.Fatalf("must write $ZDOTDIR/.zprofile, not $HOME/.zprofile, stat err: %v", err)
	}
}

// TestInstallScriptDoesNotModifyProfileForExplicitBinDir: a user-chosen
// --bin-dir off PATH gets the warning, never a profile edit — they picked
// the location; we don't second-guess their dotfiles.
func TestInstallScriptDoesNotModifyProfileForExplicitBinDir(t *testing.T) {
	f := newInstallFixture(t)
	binDir := filepath.Join(f.tmp, "chosen")

	out := f.run(t, []string{"--bin-dir", binDir}, nil, "SHELL=/bin/zsh")

	if _, err := os.Stat(filepath.Join(binDir, "extend")); err != nil {
		t.Fatalf("binary not installed into --bin-dir: %v\n%s", err, out)
	}
	if !strings.Contains(out, "is not on PATH") {
		t.Fatalf("expected the not-on-PATH warning for explicit --bin-dir; got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zprofile")); !os.IsNotExist(err) {
		t.Fatalf("explicit --bin-dir must not write a profile, stat err: %v\n%s", err, out)
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
  printf '%s\n' "$*" >> "${EXTEND_FAKE_SETUP_LOG:?}"
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
