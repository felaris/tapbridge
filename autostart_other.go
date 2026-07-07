//go:build !darwin && !windows

package main

import "errors"

func enableAutoStart() error {
	return errors.New("start at login is not supported on this platform")
}

func disableAutoStart() error {
	return nil
}

func isAutoStartEnabled() bool {
	return false
}
