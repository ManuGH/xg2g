package capabilities

const MaxSupportedVersion = 3

func IsSupportedVersion(version int) bool {
	return version >= 1 && version <= MaxSupportedVersion
}
