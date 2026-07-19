// Package wsl wraps wsl.exe for managing and driving the private
// "steamos-builder" Arch distro the app builds images inside.
package wsl

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Distro is the name of the private WSL distro used for builds.
const Distro = "steamos-builder"

const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	// The app may run from a CD/UNC path wsl.exe can't translate into a
	// working directory ("Failed to translate 'F:\'") — pin something sane.
	cmd.Dir = os.TempDir()
}

// decode handles wsl.exe management-command output, which is UTF-16LE
// (command output from inside a distro is plain UTF-8 and passes through).
func decode(b []byte) string {
	hasBOM := len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE
	if !hasBOM && bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	if hasBOM {
		b = b[2:]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// Manage runs a wsl.exe management command (--list, --import, --install, …)
// and returns its decoded combined output.
func Manage(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	hide(cmd)
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(decode(out))
	if err != nil {
		return s, fmt.Errorf("wsl %s failed: %w\n%s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

// Installed reports whether WSL is present and functional.
func Installed() bool {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return false
	}
	_, err := Manage(context.Background(), "--status")
	return err == nil
}

// DistroExists reports whether a distro with the given name is registered.
func DistroExists(name string) bool {
	out, err := Manage(context.Background(), "--list", "--quiet")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == name {
			return true
		}
	}
	return false
}

// Stream executes argv inside the builder distro as root, feeding stdin (may
// be nil) and calling onLine for every line of combined output.
func Stream(ctx context.Context, stdin io.Reader, onLine func(string), argv ...string) error {
	args := append([]string{"-d", Distro, "-u", "root", "--"}, argv...)
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	hide(cmd)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		done <- err
	}()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if onLine != nil {
			onLine(sc.Text())
		}
	}
	return <-done
}

// RunScript pipes a script into `bash -s` inside the distro (avoids all
// quoting/CRLF issues), passing args as positional parameters.
func RunScript(ctx context.Context, script string, onLine func(string), args ...string) error {
	argv := append([]string{"bash", "-s", "--"}, args...)
	return Stream(ctx, strings.NewReader(script), onLine, argv...)
}

// Output runs argv inside the distro and returns trimmed stdout (UTF-8).
func Output(ctx context.Context, argv ...string) (string, error) {
	args := append([]string{"-d", Distro, "-u", "root", "--"}, argv...)
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	hide(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %w\n%s", argv, err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// WriteFile writes content to path inside the distro (as root).
func WriteFile(ctx context.Context, path string, content []byte, mode string) error {
	script := fmt.Sprintf("mkdir -p \"$(dirname '%s')\" && cat > '%s' && chmod %s '%s'", path, path, mode, path)
	return Stream(ctx, bytes.NewReader(content), nil, "bash", "-c", script)
}

// ToWSLPath converts a Windows path to its /mnt/… form via wslpath.
func ToWSLPath(ctx context.Context, winPath string) (string, error) {
	return Output(ctx, "wslpath", "-u", winPath)
}

// UNCPaths returns candidate Windows UNC paths for a Linux path in the distro.
func UNCPaths(linuxPath string) []string {
	rel := strings.ReplaceAll(strings.TrimPrefix(linuxPath, "/"), "/", `\`)
	return []string{
		`\\wsl.localhost\` + Distro + `\` + rel,
		`\\wsl$\` + Distro + `\` + rel,
	}
}

// Terminate stops the builder distro (kills any in-flight build).
func Terminate() {
	_, _ = Manage(context.Background(), "--terminate", Distro)
}

// Unregister removes the builder distro and its disk entirely.
func Unregister() (string, error) {
	return Manage(context.Background(), "--unregister", Distro)
}
