//go:build notray

package main

// runUI always runs headless in builds without the tray (-tags notray), which
// drop the GTK/AppIndicator dependency — used by the Docker image.
func runUI(a *app, _ bool) { runHeadless(a) }
