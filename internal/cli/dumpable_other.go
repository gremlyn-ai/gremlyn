//go:build !linux

package cli

func protectProcessEnviron() error {
	return nil
}
