// Package flash enumerates USB target disks and raw-writes images to them.
package flash

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// Disk describes a candidate target disk. Only USB-bus, non-boot, non-system
// disks are ever offered.
type Disk struct {
	Number       int     `json:"number"`
	FriendlyName string  `json:"friendlyName"`
	SizeBytes    int64   `json:"sizeBytes"`
	SizeGB       float64 `json:"sizeGB"`
	BusType      string  `json:"busType"`
}

func PowerShell(script string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return nil, fmt.Errorf("powershell failed: %w\n%s", err, stderr)
	}
	return out, nil
}

// ListUSB returns USB-attached disks that are safe to offer as flash targets.
func ListUSB() ([]Disk, error) {
	out, err := PowerShell(`Get-Disk | Where-Object { $_.BusType -eq 'USB' -and -not $_.IsBoot -and -not $_.IsSystem } | Select-Object Number,FriendlyName,@{n='Size';e={[int64]$_.Size}},@{n='BusType';e={[string]$_.BusType}} | ConvertTo-Json -Compress`)
	if err != nil {
		return nil, err
	}
	txt := strings.TrimSpace(string(out))
	if txt == "" {
		return []Disk{}, nil
	}
	if strings.HasPrefix(txt, "{") {
		txt = "[" + txt + "]"
	}
	var raw []struct {
		Number       int    `json:"Number"`
		FriendlyName string `json:"FriendlyName"`
		Size         int64  `json:"Size"`
		BusType      string `json:"BusType"`
	}
	if err := json.Unmarshal([]byte(txt), &raw); err != nil {
		return nil, fmt.Errorf("parsing disk list: %w\n%s", err, txt)
	}
	disks := make([]Disk, 0, len(raw))
	for _, r := range raw {
		disks = append(disks, Disk{
			Number:       r.Number,
			FriendlyName: strings.TrimSpace(r.FriendlyName),
			SizeBytes:    r.Size,
			SizeGB:       float64(r.Size) / (1024 * 1024 * 1024),
			BusType:      r.BusType,
		})
	}
	return disks, nil
}

// driveLetters returns the mounted drive letters (e.g. "E") of a disk's
// partitions, so their volumes can be locked and dismounted before writing.
func driveLetters(diskNumber int) []string {
	out, err := PowerShell(fmt.Sprintf(
		`(Get-Partition -DiskNumber %d -ErrorAction SilentlyContinue | ForEach-Object { $_.DriveLetter }) -join ','`,
		diskNumber))
	if err != nil {
		return nil
	}
	var letters []string
	for _, tok := range strings.Split(strings.TrimSpace(string(out)), ",") {
		tok = strings.TrimSpace(tok)
		if len(tok) == 1 && tok[0] >= 'A' && tok[0] <= 'Z' {
			letters = append(letters, tok)
		}
	}
	return letters
}
