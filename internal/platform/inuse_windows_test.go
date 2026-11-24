//go:build windows

package platform

import "testing"

func TestCamUserRunning(t *testing.T) {
	images := []string{
		`C:\Program Files\Zoom\bin\Zoom.exe`,
		`C:\Program Files\WindowsApps\MSTeams_25.1.2.3_x64__8wekyb3d8bbwe\ms-teams.exe`,
		`svchost.exe`,
	}
	cases := []struct {
		u    camUser
		want bool
	}{
		{camUser{name: `C:\Program Files\Zoom\bin\Zoom.exe`}, true},
		{camUser{name: `c:\program files\zoom\bin\zoom.exe`}, true},
		{camUser{name: `C:\tools\ffmpeg.exe`}, false},
		{camUser{name: `C:\Windows\System32\svchost.exe`}, true}, // path unreadable, name matches
		{camUser{packaged: true, name: "MSTeams_8wekyb3d8bbwe"}, true},
		{camUser{packaged: true, name: "Microsoft.WindowsCamera_8wekyb3d8bbwe"}, false},
		{camUser{packaged: true, name: "MSTeams_otherpublisher"}, false},
	}
	for _, c := range cases {
		if got := c.u.running(images); got != c.want {
			t.Errorf("%+v: got %v want %v", c.u, got, c.want)
		}
	}
}
