package backup

import (
	"fmt"
	"strconv"
	"strings"
)

func normalizedVersion(version string) string {
	if len(version) > 1 && version[0] == 'v' && version[1] >= '0' && version[1] <= '9' {
		return version[1:]
	}
	return version
}

// Cross-version sources are deliberately explicit. Add a source only with a
// frozen archive and a restore regression test; an older number is not proof
// of compatibility. Same-version restores retain their existing behavior.
func checkAppVersion(source, target string) error {
	source, target = normalizedVersion(source), normalizedVersion(target)
	if source != "" && source == target {
		return nil
	}
	old, oldOK := releaseVersion(source)
	current, currentOK := releaseVersion(target)
	if oldOK && currentOK && source == "2.4.1" && old[0] == current[0] {
		for i := range old {
			if old[i] < current[i] {
				return nil
			}
			if old[i] > current[i] {
				break
			}
		}
	}
	return fmt.Errorf("backup version incompatible: PrismCat %q -> %q is not a verified upgrade path; restore with the matching source version or a supported newer release", source, target)
}

// Development and prerelease labels must match exactly. Treating them as the
// corresponding stable release could silently allow an incompatible downgrade.
func releaseVersion(version string) ([3]uint64, bool) {
	var result [3]uint64
	parts := strings.Split(version, ".")
	if len(parts) != len(result) {
		return result, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return result, false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return result, false
			}
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, false
		}
		result[i] = number
	}
	return result, true
}
