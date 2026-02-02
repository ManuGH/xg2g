package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Simplified types for reproduction
type MountInfo struct {
	MountPoint string
	FsType     string
}

type Match struct {
	Prefix string
	Info   MountInfo
}

func main() {
	// The line from user's system
	rawLine := "988 957 0:261 /enigma2-recordings /media/nfs-recordings rw,relatime shared:710 master:47 - fuse.mergerfs mergerfs rw,user_id=0,group_id=0,default_permissions,allow_other"

	mounts := make(map[string]MountInfo)

	// Simulate parseMountInfo logic
	fields := strings.Fields(rawLine)
	if len(fields) >= 10 {
		mountPoint := unescapeMountPath(fields[4])

		separatorIdx := -1
		for i, f := range fields {
			if f == "-" {
				separatorIdx = i
				break
			}
		}

		if separatorIdx != -1 && len(fields) > separatorIdx+1 {
			fsType := fields[separatorIdx+1]
			mounts[mountPoint] = MountInfo{
				MountPoint: mountPoint,
				FsType:     fsType,
			}
			fmt.Printf("Parsed: MountPoint='%s', FsType='%s'\n", mountPoint, fsType)
		}
	} else {
		fmt.Println("Failed to parse field count")
	}

	// Simulate findMountForPath logic
	searchPath := "/media/nfs-recordings" // Cleaned
	best := findMountForPath(searchPath, mounts)

	fmt.Printf("Search for '%s': Found='%s'\n", searchPath, best.MountPoint)

	if best.MountPoint == "" {
		fmt.Println("FAIL: Mount not found")
	} else {
		fmt.Println("SUCCESS: Mount found")
	}
}

func unescapeMountPath(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			isOctal := true
			for j := 1; j <= 3; j++ {
				if s[i+j] < '0' || s[i+j] > '7' {
					isOctal = false
					break
				}
			}
			if isOctal {
				if val, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
					sb.WriteByte(byte(val))
					i += 3
					continue
				}
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func findMountForPath(path string, mounts map[string]MountInfo) MountInfo {
	path = filepath.Clean(path)
	var best Match

	for mountPoint, info := range mounts {
		mp := filepath.Clean(mountPoint)
		matched := false

		if path == mp {
			matched = true
		} else if mp == "/" {
			matched = true
		} else if strings.HasPrefix(path, mp+"/") {
			matched = true
		}

		if matched {
			if len(mp) > len(best.Prefix) {
				best = Match{Prefix: mp, Info: info}
			}
		}
	}
	return best.Info
}
