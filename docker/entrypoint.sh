#!/bin/sh
# Starts pcscd (unless the host's pcscd socket is mounted), then runs
# TapBridge as the unprivileged "tapbridge" user.
set -eu

SOCKET=/run/pcscd/pcscd.comm

# Informational flags don't need the smart-card service.
case "${1:-}" in
  --version|-version|--help|-help|-h|--healthcheck|-healthcheck) exec tapbridge "$@" ;;
esac

if [ -S "$SOCKET" ]; then
  echo "[entrypoint] using the host's pcscd ($SOCKET is mounted)"
else
  if [ ! -d /dev/bus/usb ]; then
    echo "[entrypoint] WARNING: no USB devices visible. Start the container with --device /dev/bus/usb" >&2
    echo "[entrypoint]          (or mount the host's pcscd socket: -v /run/pcscd:/run/pcscd). See docs/docker.md" >&2
  fi
  # polkit isn't available in the container; access is controlled by who can
  # reach the socket, which only exists inside this container.
  POLKIT=""
  if pcscd --help 2>&1 | grep -q -- --disable-polkit; then POLKIT="--disable-polkit"; fi
  # shellcheck disable=SC2086
  pcscd $POLKIT
  echo "[entrypoint] started pcscd"
fi

if [ "$(id -u)" = "0" ]; then
  exec setpriv --reuid=tapbridge --regid=tapbridge --init-groups -- tapbridge "$@"
fi
exec tapbridge "$@"
