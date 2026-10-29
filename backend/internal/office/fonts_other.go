//go:build !darwin

package office

// fontConfig leaves font discovery to the platform configuration.
func fontConfig(string, string) (string, error) {
	return "", nil
}
