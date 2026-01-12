package v3

import (
	"path/filepath"
	"testing"
)

func TestFindMountForPath(t *testing.T) {
	mounts := map[string]MountInfo{
		"/":                     {MountPoint: "/", FsType: "ext4"},
		"/media/hdd":            {MountPoint: "/media/hdd", FsType: "ext4"},
		"/media/nfs-recordings": {MountPoint: "/media/nfs-recordings", FsType: "nfs4"},
	}

	tests := []struct {
		path     string
		expected string
	}{
		{"/media/hdd", "/media/hdd"},
		{"/media/hdd/movie", "/media/hdd"},
		{"/media/nfs-recordings/2026/test.ts", "/media/nfs-recordings"},
		{"/var/log", "/"},
		{"/non-existent", "/"},
		{"/media/My Drive", "/media/My Drive"},
		{"/media/My Drive/backup", "/media/My Drive"},
	}

	mounts["/media/My Drive"] = MountInfo{MountPoint: "/media/My Drive", FsType: "vfat"}

	for _, tt := range tests {
		m := findMountForPath(tt.path, mounts)
		if filepath.Clean(m.MountPoint) != filepath.Clean(tt.expected) {
			t.Errorf("findMountForPath(%q) = %q, expected %q", tt.path, m.MountPoint, tt.expected)
		}
	}
}

func TestUnescapeMountPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/media/hdd", "/media/hdd"},
		{"/media/My\\040Drive", "/media/My Drive"},
		{"/media/Tab\\011Dir", "/media/Tab\tDir"},
		{"/media/Backslash\\134Dir", "/media/Backslash\\Dir"},
		{"/media/Multi\\040Spaces", "/media/Multi Spaces"},
		{"/broken\\0", "/broken\\0"},
		{"/broken\\999", "/broken\\999"},
	}

	for _, tt := range tests {
		got := unescapeMountPath(tt.input)
		if got != tt.expected {
			t.Errorf("unescapeMountPath(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
