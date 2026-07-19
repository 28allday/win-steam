package flash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	fsctlLockVolume           = 0x00090018
	fsctlDismountVolume       = 0x00090020
	fsctlUnlockVolume         = 0x0009001C
	ioctlDiskUpdateProperties = 0x00070140

	chunkSize   = 4 * 1024 * 1024 // write unit
	sectorAlign = 4096            // pad final chunk to this boundary
)

func openRaw(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

func ioctl(h windows.Handle, code uint32) error {
	var ret uint32
	return windows.DeviceIoControl(h, code, nil, 0, nil, 0, &ret, nil)
}

// lockVolumes opens, locks and dismounts every mounted volume on the disk.
// The returned handles must stay open until the write finishes.
func lockVolumes(diskNumber int) ([]windows.Handle, error) {
	var handles []windows.Handle
	for _, letter := range driveLetters(diskNumber) {
		h, err := openRaw(`\\.\`+letter+`:`, windows.GENERIC_READ|windows.GENERIC_WRITE)
		if err != nil {
			continue // letterless/ghost volume — the physical-drive open will tell us
		}
		locked := false
		for i := 0; i < 10; i++ {
			if err := ioctl(h, fsctlLockVolume); err == nil {
				locked = true
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
		if !locked {
			windows.CloseHandle(h)
			return handles, fmt.Errorf("volume %s: is in use (close Explorer windows and apps using it)", letter)
		}
		_ = ioctl(h, fsctlDismountVolume)
		handles = append(handles, h)
	}
	return handles, nil
}

func releaseVolumes(handles []windows.Handle) {
	for _, h := range handles {
		_ = ioctl(h, fsctlUnlockVolume)
		windows.CloseHandle(h)
	}
}

// diskpartClean wipes the partition table so Windows releases every volume.
// Only used as a fallback when the plain locked write can't start — the disk
// is about to be overwritten anyway.
func diskpartClean(diskNumber int) error {
	cmd := exec.Command("diskpart.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Stdin = strings.NewReader(fmt.Sprintf("select disk %d\r\nclean\r\nrescan\r\n", diskNumber))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskpart clean failed: %w\n%s", err, out)
	}
	return nil
}

// Flash raw-writes imgPath onto \\.\PhysicalDrive<diskNumber>, reporting
// progress. The image may live on a \\wsl.localhost\ UNC path.
func Flash(ctx context.Context, diskNumber int, imgPath string, progress func(written, total int64)) error {
	src, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("opening image: %w", err)
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	total := st.Size()

	drivePath := fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNumber)

	vols, lockErr := lockVolumes(diskNumber)
	h, err := openRaw(drivePath, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil || lockErr != nil {
		// Something still owns the disk — nuke the partition table and retry.
		releaseVolumes(vols)
		if h != 0 && h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
		if dpErr := diskpartClean(diskNumber); dpErr != nil {
			if lockErr != nil {
				return lockErr
			}
			return fmt.Errorf("cannot open %s: %v (and %v)", drivePath, err, dpErr)
		}
		time.Sleep(2 * time.Second)
		vols = nil
		h, err = openRaw(drivePath, windows.GENERIC_READ|windows.GENERIC_WRITE)
		if err != nil {
			return fmt.Errorf("cannot open %s even after clean: %w", drivePath, err)
		}
	}
	defer func() {
		releaseVolumes(vols)
		_ = ioctl(h, ioctlDiskUpdateProperties) // make Windows rescan the new partition table
		windows.CloseHandle(h)
	}()

	buf := make([]byte, chunkSize)
	var written int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, rerr := io_readFull(src, buf)
		if n > 0 {
			// Writes to a disk device must be sector-multiple sized.
			wlen := n
			if rem := wlen % sectorAlign; rem != 0 {
				pad := sectorAlign - rem
				for i := wlen; i < wlen+pad; i++ {
					buf[i] = 0
				}
				wlen += pad
			}
			var done uint32
			if werr := windows.WriteFile(h, buf[:wlen], &done, nil); werr != nil {
				return fmt.Errorf("write failed at %d MiB: %w", written/(1024*1024), werr)
			}
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if rerr != nil {
			break
		}
	}
	if written < total {
		return fmt.Errorf("short write: %d of %d bytes", written, total)
	}
	if err := windows.FlushFileBuffers(h); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}
	return nil
}

// io_readFull reads up to len(buf); returns bytes read and a non-nil error
// only at EOF or on failure.
func io_readFull(f *os.File, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
