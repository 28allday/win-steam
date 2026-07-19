package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sngi/internal/flash"
	"sngi/internal/wsl"
)

//go:embed scripts/builder-setup.sh scripts/run-build.sh scripts/steamos-nvidia-installer.sh
var scripts embed.FS

const (
	// The recovery image must be downloaded by the user from Valve's help
	// page (they have to accept Valve's terms there) — the app only opens
	// the page and takes the downloaded file.
	valveHelpURL   = "https://help.steampowered.com/en/faqs/view/65B4-2AA3-5F37-4227#install"
	archWSLURL     = "https://geo.mirror.pkgbuild.com/wsl/latest/archlinux.wsl"
	readyMarker    = "/opt/steamos-nvidia/.ready"
	minFreeSpaceGB = 30.0
	minWinBuild    = 19041 // Windows 10 2004 — first WSL2 release
)

// App is the Wails-bound application backend.
type App struct {
	ctx context.Context

	mu     sync.Mutex
	busy   string             // name of the running operation, "" if idle
	cancel context.CancelFunc // cancels the running operation

	outputLinux string // /build/…-nvidia-usbinstall.img inside the distro
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// ---------------------------------------------------------------- events

type evt struct {
	Chan  string `json:"chan"` // wsl | setup | build | flash
	Type  string `json:"type"` // log | progress | done | error
	Msg   string `json:"msg,omitempty"`
	Cur   int64  `json:"cur,omitempty"`
	Total int64  `json:"total,omitempty"`
}

func (a *App) emit(e evt) { runtime.EventsEmit(a.ctx, "evt", e) }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// cleanLine strips ANSI colour codes and keeps only the final \r segment of
// progress-style lines (pacman, curl).
func cleanLine(s string) string {
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimRight(ansiRE.ReplaceAllString(s, ""), " ")
}

func (a *App) logTo(ch string) func(string) {
	return func(line string) {
		if l := cleanLine(line); l != "" {
			a.emit(evt{Chan: ch, Type: "log", Msg: l})
		}
	}
}

// begin claims the single-operation slot; returns a context or an error if busy.
func (a *App) begin(name string) (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy != "" {
		return nil, fmt.Errorf("another operation is running: %s", a.busy)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.busy, a.cancel = name, cancel
	return ctx, nil
}

func (a *App) end() {
	a.mu.Lock()
	a.busy, a.cancel = "", nil
	a.mu.Unlock()
}

// Cancel aborts the running operation (and any in-flight WSL build with it).
func (a *App) Cancel() {
	a.mu.Lock()
	cancel := a.cancel
	busy := a.busy
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if busy == "build" || busy == "setup" {
		wsl.Terminate()
	}
}

// ---------------------------------------------------------- system check

type SystemStatus struct {
	IsAdmin      bool     `json:"isAdmin"`
	WindowsBuild uint32   `json:"windowsBuild"`
	WindowsOK    bool     `json:"windowsOK"`
	WSLInstalled bool     `json:"wslInstalled"`
	BuilderReady bool     `json:"builderReady"`
	FreeSpaceGB  float64  `json:"freeSpaceGB"`
	SpaceOK      bool     `json:"spaceOK"`
	VirtOK       bool     `json:"virtOK"`
	VirtInfo     string   `json:"virtInfo"`
	SModeOK      bool     `json:"sModeOK"`
	Warnings     []string `json:"warnings"`
}

// checkVirt reports whether the CPU's virtualization support is usable:
// either a hypervisor is already active (WSL2/Hyper-V running) or the
// firmware toggle (VT-x / AMD-V "SVM") is enabled and waiting.
func checkVirt() (bool, string) {
	out, err := flash.PowerShell(`@{h=[bool](Get-CimInstance Win32_ComputerSystem).HypervisorPresent; v=[bool](Get-CimInstance Win32_Processor | Select-Object -First 1).VirtualizationFirmwareEnabled} | ConvertTo-Json -Compress`)
	if err != nil {
		return true, "could not determine" // don't block on a failed probe
	}
	var r struct {
		H bool `json:"h"`
		V bool `json:"v"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(string(out))), &r) != nil {
		return true, "could not determine"
	}
	switch {
	case r.H:
		return true, "hypervisor active"
	case r.V:
		return true, "enabled in firmware"
	default:
		return false, "disabled in UEFI/BIOS"
	}
}

// inSMode reports whether Windows is running in S Mode (Store-only apps).
// Largely theoretical here — S Mode wouldn't have launched this exe at all.
func inSMode() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\CI\Policy`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SkuPolicyRequired")
	return err == nil && v == 1
}

func (a *App) CheckSystem() SystemStatus {
	s := SystemStatus{Warnings: []string{}}

	s.IsAdmin = windows.GetCurrentProcessToken().IsElevated()
	if !s.IsAdmin {
		s.Warnings = append(s.Warnings, "Not running as Administrator — WSL setup and USB flashing will fail. Restart the app via 'Run as administrator'.")
	}

	ver := windows.RtlGetVersion()
	s.WindowsBuild = ver.BuildNumber
	s.WindowsOK = ver.BuildNumber >= minWinBuild
	if !s.WindowsOK {
		s.Warnings = append(s.Warnings, fmt.Sprintf("Windows build %d is too old for WSL2 (need %d+ / Windows 10 2004).", ver.BuildNumber, minWinBuild))
	}

	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	var free, totalB, totalFree uint64
	if p, err := windows.UTF16PtrFromString(sysDrive + `\`); err == nil {
		if err := windows.GetDiskFreeSpaceEx(p, &free, &totalB, &totalFree); err == nil {
			s.FreeSpaceGB = float64(free) / (1024 * 1024 * 1024)
			s.SpaceOK = s.FreeSpaceGB >= minFreeSpaceGB
		}
	}
	if !s.SpaceOK {
		s.Warnings = append(s.Warnings, fmt.Sprintf("Only %.0f GB free on %s — the build needs about %.0f GB.", s.FreeSpaceGB, sysDrive, minFreeSpaceGB))
	}

	s.WSLInstalled = wsl.Installed()

	if s.WSLInstalled {
		// A working WSL proves virtualization works, whatever WMI claims.
		s.VirtOK, s.VirtInfo = true, "working (WSL2 runs)"
	} else {
		s.VirtOK, s.VirtInfo = checkVirt()
		if !s.VirtOK {
			s.Warnings = append(s.Warnings, "CPU virtualization is disabled in the UEFI/BIOS — WSL2 cannot be installed until it's enabled. Reboot into the UEFI setup (usually Del or F2 at power-on) and enable 'Intel VT-x' / 'AMD SVM' / 'Virtualization Technology', then re-check.")
		}
	}

	s.SModeOK = !inSMode()
	if !s.SModeOK {
		s.Warnings = append(s.Warnings, "Windows is in S Mode, which only allows Store apps. Switch out of S Mode (Settings → Activation → 'Switch to Windows 11 Home') and try again.")
	}

	if s.WSLInstalled && wsl.DistroExists(wsl.Distro) {
		if _, err := wsl.Output(context.Background(), "test", "-f", readyMarker); err == nil {
			s.BuilderReady = true
		}
	}
	return s
}

// --------------------------------------------------------- WSL install

func (a *App) StartWSLInstall() error {
	ctx, err := a.begin("wsl")
	if err != nil {
		return err
	}
	go func() {
		defer a.end()
		log := a.logTo("wsl")
		log("Installing WSL2 (this can take several minutes)...")
		out, err := wsl.Manage(ctx, "--install", "--no-distribution")
		for _, l := range strings.Split(out, "\n") {
			if l = cleanLine(l); l != "" {
				log(l)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				a.emit(evt{Chan: "wsl", Type: "error", Msg: "WSL install cancelled."})
				return
			}
			// The inbox wsl.exe stub often can't self-install (rejects
			// --no-distribution, Store unavailable, …). Standalone path:
			// enable the VM Platform feature + install Microsoft's WSL MSI.
			log("Built-in installer refused — using the standalone WSL installer instead.")
			if err := a.installWSLStandalone(ctx, log); err != nil {
				a.emit(evt{Chan: "wsl", Type: "error", Msg: "WSL install failed: " + err.Error()})
				return
			}
		}
		a.emit(evt{Chan: "wsl", Type: "done", Msg: "WSL installed. RESTART Windows now (Start → Power → Restart — a plain shut-down is not enough), then reopen SNGI."})
	}()
	return nil
}

// rebootOK treats exit code 3010 (ERROR_SUCCESS_REBOOT_REQUIRED) from
// dism/msiexec as success.
func rebootOK(err error) bool {
	if err == nil {
		return true
	}
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 3010
}

func (a *App) runHidden(ctx context.Context, log func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	for _, l := range strings.Split(string(out), "\n") {
		if l = cleanLine(l); l != "" {
			log(l)
		}
	}
	return err
}

// installWSLStandalone enables VirtualMachinePlatform and installs the
// current WSL MSI from Microsoft's GitHub releases — no Store, no stub.
func (a *App) installWSLStandalone(ctx context.Context, log func(string)) error {
	log("Enabling the Virtual Machine Platform feature...")
	if err := a.runHidden(ctx, log, "dism.exe", "/online", "/enable-feature",
		"/featurename:VirtualMachinePlatform", "/all", "/norestart"); !rebootOK(err) {
		return fmt.Errorf("enabling VirtualMachinePlatform failed: %v", err)
	}

	log("Finding the latest WSL release on Microsoft's GitHub...")
	body, err := fetchBytes(ctx, "https://api.github.com/repos/microsoft/WSL/releases/latest", 2<<20)
	if err != nil {
		return fmt.Errorf("querying WSL releases: %w", err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("parsing WSL release info: %w", err)
	}
	msiURL := ""
	for _, as := range rel.Assets {
		if strings.HasSuffix(as.Name, ".x64.msi") {
			msiURL = as.URL
			break
		}
	}
	if msiURL == "" {
		return fmt.Errorf("no x64 MSI in WSL release %s", rel.TagName)
	}

	log(fmt.Sprintf("Downloading WSL %s...", rel.TagName))
	dest := filepath.Join(os.TempDir(), "wsl-installer.msi")
	if err := a.download(ctx, msiURL, dest, "wsl"); err != nil {
		return fmt.Errorf("downloading WSL MSI: %w", err)
	}

	log("Installing WSL (silent)...")
	if err := a.runHidden(ctx, log, "msiexec.exe", "/i", dest, "/qn", "/norestart"); !rebootOK(err) {
		return fmt.Errorf("WSL MSI install failed: %v", err)
	}
	os.Remove(dest)
	log("WSL installed.")
	return nil
}

func fetchBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s from %s", resp.Status, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// --------------------------------------------------------- builder setup

func (a *App) StartSetup() error {
	ctx, err := a.begin("setup")
	if err != nil {
		return err
	}
	go func() {
		defer a.end()
		if err := a.setupBuilder(ctx); err != nil {
			if ctx.Err() != nil {
				a.emit(evt{Chan: "setup", Type: "error", Msg: "Setup cancelled."})
			} else {
				a.emit(evt{Chan: "setup", Type: "error", Msg: err.Error()})
			}
			return
		}
		a.emit(evt{Chan: "setup", Type: "done", Msg: "Builder ready."})
	}()
	return nil
}

func (a *App) setupBuilder(ctx context.Context) error {
	log := a.logTo("setup")

	if !wsl.DistroExists(wsl.Distro) {
		log("Getting Arch Linux for the builder (~350 MB download)...")
		// Preferred: let WSL fetch it from the Microsoft distro catalog.
		out, err := wsl.Manage(ctx, "--install", "archlinux", "--name", wsl.Distro, "--no-launch")
		for _, l := range strings.Split(out, "\n") {
			if l = cleanLine(l); l != "" {
				log(l)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log("WSL catalog install unavailable — downloading the image from Arch's mirror instead.")
			if err := a.importFromMirror(ctx, log); err != nil {
				return err
			}
		}
		if !wsl.DistroExists(wsl.Distro) {
			// A fresh WSL install can defer everything until the next boot:
			// "The requested operation is successful. Changes will not be
			// effective until the system is rebooted."
			low := strings.ToLower(out)
			if strings.Contains(low, "reboot") || strings.Contains(low, "restart") {
				return fmt.Errorf("Windows needs a RESTART to finish setting up WSL (use Start → Power → Restart — a plain shut-down is not enough, Fast Startup skips the step). Then reopen SNGI and run this step again — it continues where it left off.")
			}
			return fmt.Errorf("distro %s did not register — see log above", wsl.Distro)
		}
	} else {
		log("Builder distro already exists — resuming setup.")
	}

	log("Preparing the builder (package updates + build tools)...")
	setup, _ := scripts.ReadFile("scripts/builder-setup.sh")
	if err := wsl.RunScript(ctx, string(setup), log); err != nil {
		return fmt.Errorf("builder setup failed: %w", err)
	}

	log("Installing build scripts into the builder...")
	if err := pushScripts(ctx); err != nil {
		return err
	}
	if err := wsl.Stream(ctx, nil, nil, "touch", readyMarker); err != nil {
		return err
	}
	return nil
}

// importFromMirror downloads the official Arch WSL image and imports it.
func (a *App) importFromMirror(ctx context.Context, log func(string)) error {
	dest := filepath.Join(os.TempDir(), "archlinux.wsl")
	if err := a.download(ctx, archWSLURL, dest, "setup"); err != nil {
		return fmt.Errorf("downloading Arch WSL image: %w", err)
	}
	// Best-effort checksum against the mirror's published SHA256.
	if sum, err := fetchString(ctx, archWSLURL+".SHA256"); err == nil {
		want := strings.Fields(sum)
		if len(want) > 0 {
			got, err := fileSHA256(dest)
			if err == nil && !strings.EqualFold(got, want[0]) {
				return fmt.Errorf("Arch WSL image checksum mismatch (got %s, want %s) — retry the setup", got, want[0])
			}
			log("Checksum verified.")
		}
	} else {
		log("Could not fetch checksum file — continuing (download was HTTPS).")
	}

	instDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "steamos-builder")
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		return err
	}
	log("Importing the builder distro...")
	if _, err := wsl.Manage(ctx, "--import", wsl.Distro, instDir, dest, "--version", "2"); err != nil {
		// Older WSL builds can't import the zstd-compressed .wsl format.
		if _, err2 := wsl.Manage(ctx, "--install", "--from-file", dest, "--name", wsl.Distro, "--no-launch"); err2 != nil {
			return fmt.Errorf("could not import the Arch image.\n--import said: %v\n--install --from-file said: %v\nTry running 'wsl --update' in PowerShell, then retry", err, err2)
		}
	}
	os.Remove(dest)
	return nil
}

func (a *App) download(ctx context.Context, url, dest, ch string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s from %s", resp.Status, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	total := resp.ContentLength
	buf := make([]byte, 1024*1024)
	var got int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			got += int64(n)
			a.emit(evt{Chan: ch, Type: "progress", Cur: got, Total: total})
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func fetchString(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(b), err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ------------------------------------------------------------ image pick

type ImagePick struct {
	Path   string  `json:"path"`
	Name   string  `json:"name"`
	SizeGB float64 `json:"sizeGB"`
}

func (a *App) ChooseImage() (*ImagePick, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose the SteamOS recovery image you downloaded from Valve",
		Filters: []runtime.FileFilter{
			{DisplayName: "SteamOS recovery image (*.img;*.img.bz2)", Pattern: "*.img;*.bz2"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	return a.InspectImage(path)
}

// InspectImage validates a candidate image path (file dialog or drag-drop).
func (a *App) InspectImage(path string) (*ImagePick, error) {
	low := strings.ToLower(path)
	if !strings.HasSuffix(low, ".img") && !strings.HasSuffix(low, ".img.bz2") {
		return nil, fmt.Errorf("that doesn't look like a SteamOS recovery image (.img or .img.bz2)")
	}
	if strings.Contains(strings.ToLower(filepath.Base(path)), "-nvidia") {
		return nil, fmt.Errorf("that looks like an already-patched image — pick the clean recovery image from Valve")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &ImagePick{
		Path:   path,
		Name:   filepath.Base(path),
		SizeGB: float64(st.Size()) / (1024 * 1024 * 1024),
	}, nil
}

func (a *App) OpenValveHelp() {
	runtime.BrowserOpenURL(a.ctx, valveHelpURL)
}

// ----------------------------------------------------------------- build

type BuildOptions struct {
	ImagePath  string `json:"imagePath"`
	UpdateMode string `json:"updateMode"` // selfheal | hold | stock
	TrimCuda   bool   `json:"trimCuda"`
	SkipSig    bool   `json:"skipSig"`
}

func (a *App) StartBuild(opts BuildOptions) error {
	ctx, err := a.begin("build")
	if err != nil {
		return err
	}
	go func() {
		defer a.end()
		if err := a.runBuild(ctx, opts); err != nil {
			if ctx.Err() != nil {
				a.emit(evt{Chan: "build", Type: "error", Msg: "Build cancelled."})
			} else {
				a.emit(evt{Chan: "build", Type: "error", Msg: err.Error()})
			}
			return
		}
		a.emit(evt{Chan: "build", Type: "done", Msg: a.outputLinux})
	}()
	return nil
}

// progressReader reports bytes read through it (throttled).
type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	last  int64
	emit  func(cur, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.done-p.last >= 64*1024*1024 || (err != nil && p.done != p.last) {
		p.last = p.done
		p.emit(p.done, p.total)
	}
	return n, err
}

// copyImageIn streams the image file into the distro over stdin — works no
// matter what drive it lives on (CD, USB, network — WSL only automounts
// fixed drives, so wslpath/drvfs cannot be relied on).
func (a *App) copyImageIn(ctx context.Context, winPath string, log func(string)) (string, error) {
	f, err := os.Open(winPath)
	if err != nil {
		return "", fmt.Errorf("opening image: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	name := filepath.Base(winPath)
	dst := "/build/" + name

	if out, err := wsl.Output(ctx, "stat", "-c", "%s", dst); err == nil && out == fmt.Sprint(st.Size()) {
		log("Image already inside the builder — reusing it.")
		return dst, nil
	}

	log(fmt.Sprintf("Copying %s into the builder (%.2f GB)...", name, float64(st.Size())/(1<<30)))
	pr := &progressReader{r: f, total: st.Size(), emit: func(cur, total int64) {
		a.emit(evt{Chan: "build", Type: "progress", Cur: cur, Total: total})
	}}
	script := fmt.Sprintf("mkdir -p /build && cat > '%s.part' && mv '%s.part' '%s'", dst, dst, dst)
	if err := wsl.Stream(ctx, pr, nil, "bash", "-c", script); err != nil {
		return "", fmt.Errorf("copying image into the builder: %w", err)
	}
	a.emit(evt{Chan: "build", Type: "progress", Cur: st.Size(), Total: st.Size()})
	log("Copy done.")
	return dst, nil
}

// pushScripts (re)installs the embedded build scripts into the distro. It
// runs during setup and again before every build, so an updated app never
// drives an existing builder with stale scripts.
func pushScripts(ctx context.Context) error {
	for _, f := range []struct{ src, dst string }{
		{"scripts/steamos-nvidia-installer.sh", "/opt/steamos-nvidia/steamos-nvidia-installer.sh"},
		{"scripts/run-build.sh", "/opt/steamos-nvidia/run-build.sh"},
	} {
		b, _ := scripts.ReadFile(f.src)
		if err := wsl.WriteFile(ctx, f.dst, b, "755"); err != nil {
			return fmt.Errorf("installing %s: %w", f.dst, err)
		}
	}
	return nil
}

func (a *App) runBuild(ctx context.Context, opts BuildOptions) error {
	log := a.logTo("build")

	if err := pushScripts(ctx); err != nil {
		return err
	}
	wslPath, err := a.copyImageIn(ctx, opts.ImagePath, log)
	if err != nil {
		return err
	}

	args := []string{"bash", "/opt/steamos-nvidia/run-build.sh", wslPath}
	switch opts.UpdateMode {
	case "hold":
		args = append(args, "--hold-updates")
	case "stock":
		args = append(args, "--no-hold-updates")
	}
	if opts.TrimCuda {
		args = append(args, "--trim-cuda")
	}
	if opts.SkipSig {
		args = append(args, "--skip-sigcheck")
	}

	var output string
	capture := func(line string) {
		if l := cleanLine(line); strings.HasPrefix(l, "OUTPUT_IMAGE=") {
			output = strings.TrimPrefix(l, "OUTPUT_IMAGE=")
			return
		}
		log(line)
	}
	if err := wsl.Stream(ctx, nil, capture, args...); err != nil {
		return fmt.Errorf("build failed — see log above. (%v)", err)
	}
	if output == "" {
		return fmt.Errorf("build finished but no output image was reported")
	}
	a.outputLinux = output
	return nil
}

// ----------------------------------------------------------------- flash

type OutputInfo struct {
	LinuxPath string  `json:"linuxPath"`
	Name      string  `json:"name"`
	SizeGB    float64 `json:"sizeGB"`
}

// GetBuildOutput finds a finished image in the builder (survives app restarts).
func (a *App) GetBuildOutput() (*OutputInfo, error) {
	path := a.outputLinux
	if path == "" {
		if !wsl.DistroExists(wsl.Distro) {
			return nil, nil
		}
		out, err := wsl.Output(context.Background(), "bash", "-c",
			"ls -1t /build/*-nvidia-usbinstall.img 2>/dev/null | head -1")
		if err != nil || out == "" {
			return nil, nil
		}
		path = out
		a.outputLinux = path
	}
	_, st, err := resolveUNC(path)
	if err != nil {
		return nil, err
	}
	return &OutputInfo{
		LinuxPath: path,
		Name:      path[strings.LastIndexByte(path, '/')+1:],
		SizeGB:    float64(st.Size()) / (1024 * 1024 * 1024),
	}, nil
}

func resolveUNC(linuxPath string) (string, os.FileInfo, error) {
	var lastErr error
	for _, p := range wsl.UNCPaths(linuxPath) {
		st, err := os.Stat(p)
		if err == nil {
			return p, st, nil
		}
		lastErr = err
	}
	return "", nil, fmt.Errorf("cannot reach the built image through \\\\wsl.localhost: %v", lastErr)
}

func (a *App) ListDisks() ([]flash.Disk, error) {
	return flash.ListUSB()
}

func (a *App) StartFlash(diskNumber int) error {
	ctx, err := a.begin("flash")
	if err != nil {
		return err
	}
	go func() {
		defer a.end()
		if err := a.runFlash(ctx, diskNumber); err != nil {
			if ctx.Err() != nil {
				a.emit(evt{Chan: "flash", Type: "error", Msg: "Flash cancelled — the USB stick is in an undefined state; reflash it before use."})
			} else {
				a.emit(evt{Chan: "flash", Type: "error", Msg: err.Error()})
			}
			return
		}
		a.emit(evt{Chan: "flash", Type: "done", Msg: "USB installer written. Safe to remove the stick."})
	}()
	return nil
}

func (a *App) runFlash(ctx context.Context, diskNumber int) error {
	if a.outputLinux == "" {
		return fmt.Errorf("no built image — run the build step first")
	}
	unc, st, err := resolveUNC(a.outputLinux)
	if err != nil {
		return err
	}
	disks, err := flash.ListUSB()
	if err != nil {
		return err
	}
	var target *flash.Disk
	for i := range disks {
		if disks[i].Number == diskNumber {
			target = &disks[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("disk %d is not a USB disk (or was unplugged) — refresh the list", diskNumber)
	}
	if target.SizeBytes < st.Size() {
		return fmt.Errorf("%s is too small: %.1f GB image onto a %.1f GB disk",
			target.FriendlyName, float64(st.Size())/(1<<30), target.SizeGB)
	}
	a.emit(evt{Chan: "flash", Type: "log",
		Msg: fmt.Sprintf("Writing %.1f GB to disk %d: %s", float64(st.Size())/(1<<30), diskNumber, target.FriendlyName)})
	return flash.Flash(ctx, diskNumber, unc, func(written, total int64) {
		a.emit(evt{Chan: "flash", Type: "progress", Cur: written, Total: total})
	})
}

// -------------------------------------------------------------- cleanup

// RemoveBuilder unregisters the builder distro, freeing ~20 GB.
func (a *App) RemoveBuilder() (string, error) {
	a.mu.Lock()
	if a.busy != "" {
		a.mu.Unlock()
		return "", fmt.Errorf("cannot remove the builder while %s is running", a.busy)
	}
	a.mu.Unlock()
	if !wsl.DistroExists(wsl.Distro) {
		return "Builder was not installed.", nil
	}
	a.outputLinux = ""
	if _, err := wsl.Unregister(); err != nil {
		return "", err
	}
	return "Builder removed.", nil
}
