//go:build linux

package platform

import "syscall"

// LocalVolume reports whether path sits on a locally attached filesystem.
// Unknown and network filesystem types return false: an unproven mount is
// never treated as local.
func LocalVolume(path string) (bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, err
	}
	switch uint64(st.Type) {
	case 0xEF53, // ext2/3/4
		0x58465342, // xfs
		0x9123683E, // btrfs
		0x01021994, // tmpfs
		0x2FC12FC1, // zfs
		0x2011BAB0, // exfat
		0x4d44:     // msdos
		return true, nil
	default:
		return false, nil
	}
}
