//go:build !darwin && !windows

package autostart

import "errors"

func Enable() error {
	return errors.New("start at login is not supported on this platform")
}

func Disable() error {
	return nil
}

func IsEnabled() bool {
	return false
}
