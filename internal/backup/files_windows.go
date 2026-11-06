package backup

import "golang.org/x/sys/windows"

func availableSpace(dir string) (uint64, error) {
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available uint64
	err = windows.GetDiskFreeSpaceEx(name, &available, nil, nil)
	return available, err
}
func replaceFile(source, target string) error { return windows.Rename(source, target) }
func syncDirectory(string) error              { return nil }
