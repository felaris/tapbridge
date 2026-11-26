# Running TapBridge in Docker

The `felaris/tapbridge` image runs TapBridge **headless** (no tray) with the
Linux smart-card service (`pcscd` + `libccid`) built in. Images are published
for `linux/amd64` and `linux/arm64`, so they also run on a Raspberry Pi 4/5
with a 64-bit OS.

## Where Docker makes sense

| Host | Use Docker? |
|---|---|
| **Linux** (desktop, server, kiosk, Raspberry Pi) | ✅ Yes. Pass the USB reader in with `--device` |
| **Windows** | ⚠️ Possible but fiddly: Docker Desktop runs containers in a VM, so you must forward the reader into WSL2 with [usbipd-win](https://github.com/dorssel/usbipd-win). The native `tapbridge-windows.exe` is much simpler |
| **macOS** | ❌ No. Docker Desktop on macOS can't pass USB devices to containers. Use the native app |

A container can only reach a USB device the host shares with it, so Docker
won't make a reader work "everywhere". It is a good fit for Linux machines
and headless deployments.

## Quick start (Linux)

```bash
docker run -d --name tapbridge --restart unless-stopped \
  --device /dev/bus/usb \
  -p 127.0.0.1:8765:8765 \
  felaris/tapbridge
```

Then connect your web app to `ws://localhost:8765` as usual, and watch the logs:

```bash
docker logs -f tapbridge
```

> **Keep `127.0.0.1:` in the `-p` option.** Inside the container TapBridge
> listens on all interfaces so the port mapping works. Publishing to
> `127.0.0.1` keeps it private to this machine. Plain `-p 8765:8765` exposes
> card data to your whole network.

### Docker Compose

```yaml
services:
  tapbridge:
    image: felaris/tapbridge:latest
    restart: unless-stopped
    devices:
      - /dev/bus/usb:/dev/bus/usb
    ports:
      - "127.0.0.1:8765:8765"
    environment:
      TAPBRIDGE_ALLOWED_ORIGINS: "http://localhost,https://app.example.com"
    volumes:
      - tapbridge-data:/data
      - /run/udev:/run/udev:ro   # optional: reader hot-plug detection

volumes:
  tapbridge-data:
```

## Configuration

Use the same environment variables as the desktop app (see
[configuration.md](configuration.md)):

| Variable | Example |
|---|---|
| `TAPBRIDGE_ALLOWED_ORIGINS` | `http://localhost,https://app.example.com` |
| `TAPBRIDGE_READER` | `ACS ACR122U PICC Interface 00 00` (when several readers are attached) |
| `TAPBRIDGE_PORT` | `8765` (port inside the container) |

Flags work too: `docker run … felaris/tapbridge --allow-origin https://app.example.com`.

The config file and `tapbridge.log` live in the `/data` volume.

## Linux host notes

### Free the reader from the kernel NFC driver (ACR122U)

Linux's own NFC stack (`pn533_usb`) grabs the ACR122U, which stops pcscd
from using it. Unload and blacklist it on the **host**:

```bash
sudo modprobe -r pn533_usb pn533 nfc
```

```bash
echo -e "blacklist pn533_usb\nblacklist pn533\nblacklist nfc" | sudo tee /etc/modprobe.d/blacklist-nfc.conf
```

### Don't run two pcscd's

If the host also runs `pcscd`, it holds the reader and the container's pcscd
can't open it. Either stop the host's service:

```bash
sudo systemctl disable --now pcscd.socket pcscd.service
```

or reuse the host's pcscd instead of the bundled one, by mounting its socket
(no `--device` needed):

```bash
docker run -d --name tapbridge -v /run/pcscd:/run/pcscd -p 127.0.0.1:8765:8765 felaris/tapbridge
```

The host's polkit rules may refuse the container's unprivileged user. If the
logs show a permission error, use the bundled pcscd instead.

### Hot-plug

To notice a reader plugged in **after** the container started, mount the
host's udev state: `-v /run/udev:/run/udev:ro`. Otherwise restart the
container after plugging the reader in.

## Health check

The image has a built-in `HEALTHCHECK` (`tapbridge --healthcheck`), so
`docker ps` shows `healthy` once the WebSocket server answers.

## Security

- TapBridge runs as the unprivileged user `tapbridge` (UID 10001). Only
  `pcscd` runs as root, because it needs raw USB access.
- Only `/dev/bus/usb` is shared. **Don't** use `--privileged`.
- Images are built by GitHub Actions from this repository, with signed
  build provenance and an SBOM attached. You can inspect them with:
  ```bash
  docker buildx imagetools inspect felaris/tapbridge:latest --format '{{ json .Provenance }}'
  ```

## Tags

| Tag | Meaning |
|---|---|
| `latest` | Newest release |
| `1.2.3` | An exact release |
| `1.2` | Newest patch of a minor version |

## Building it yourself

```bash
docker build -t tapbridge .
```

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t you/tapbridge --push .
```
