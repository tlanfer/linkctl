//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows records camera access per app under the privacy ConsentStore:
// LastUsedTimeStop is 0 while the app has the camera open. Entries go stale
// when an app is killed, so an entry only counts if its app is running.
const consentStore = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\webcam`

type camUser struct {
	packaged bool
	name     string // exe path (desktop app) or package family name
}

func inUse() (bool, error) {
	users, err := activeCameraUsers()
	if err != nil || len(users) == 0 {
		return false, err
	}
	procs := runningImages()
	for _, u := range users {
		if u.running(procs) {
			return true, nil
		}
	}
	return false, nil
}

func activeCameraUsers() ([]camUser, error) {
	root, err := registry.OpenKey(registry.CURRENT_USER, consentStore, registry.READ)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, err
	}
	defer root.Close()
	var out []camUser
	names, _ := root.ReadSubKeyNames(-1)
	for _, n := range names {
		if n == "NonPackaged" {
			np, err := registry.OpenKey(root, n, registry.READ)
			if err != nil {
				continue
			}
			apps, _ := np.ReadSubKeyNames(-1)
			for _, a := range apps {
				if active(np, a) {
					out = append(out, camUser{name: strings.ReplaceAll(a, "#", `\`)})
				}
			}
			np.Close()
			continue
		}
		if active(root, n) {
			out = append(out, camUser{packaged: true, name: n})
		}
	}
	return out, nil
}

func active(parent registry.Key, name string) bool {
	k, err := registry.OpenKey(parent, name, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	start, _, err1 := k.GetIntegerValue("LastUsedTimeStart")
	stop, _, err2 := k.GetIntegerValue("LastUsedTimeStop")
	return err1 == nil && err2 == nil && start != 0 && stop == 0
}

func (u camUser) running(images []string) bool {
	if !u.packaged {
		for _, img := range images {
			if strings.EqualFold(img, u.name) {
				return true
			}
		}
		// Could not read every process path (e.g. elevated processes): fall
		// back to matching the file name.
		base := filepath.Base(u.name)
		for _, img := range images {
			if !strings.Contains(img, `\`) && strings.EqualFold(img, base) {
				return true
			}
		}
		return false
	}
	// Package family name "Name_PublisherId"; installed under
	// WindowsApps\Name_Version_Arch__PublisherId\.
	i := strings.LastIndex(u.name, "_")
	if i <= 0 {
		return false
	}
	pkg, pub := strings.ToLower(u.name[:i]), strings.ToLower(u.name[i+1:])
	for _, img := range images {
		l := strings.ToLower(img)
		j := strings.Index(l, `\windowsapps\`+pkg+"_")
		if j >= 0 && strings.Contains(l[j:], "__"+pub+`\`) {
			return true
		}
	}
	return false
}

// runningImages returns the full image path of every process it may query,
// or the bare exe name where the path is not accessible.
func runningImages() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []string
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := windows.UTF16ToString(e.ExeFile[:])
		if p := imagePath(e.ProcessID); p != "" {
			name = p
		}
		out = append(out, name)
	}
	return out
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
