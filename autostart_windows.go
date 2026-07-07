//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runValueName = "TapBridge"
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
)

func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Quote the path so Windows treats a path containing spaces (e.g.
	// "C:\Program Files\..." or "C:\Users\Jane Doe\...") as a single argument
	// rather than launching only the first token at login.
	return k.SetStringValue(runValueName, `"`+exe+`"`)
}

func disableAutoStart() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer k.Close()
	err = k.DeleteValue(runValueName)
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}
