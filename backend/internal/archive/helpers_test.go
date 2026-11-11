package archive

// inspectMembers is InspectWithSkipped for tests that do not need the skipped list.
func inspectMembers(zipPath string, lim Limits) ([]Member, error) {
	members, _, err := InspectWithSkipped(zipPath, lim)
	return members, err
}
