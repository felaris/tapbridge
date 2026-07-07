## Summary

<!-- What does this PR change and why? -->

## Related issue

<!-- e.g. Closes #12 -->

## Checklist

- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `go build ./...` passes
- [ ] Cross-compiles for Windows (`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null .`) — if touching platform code
- [ ] README updated if user-facing behavior, flags, or protocol changed
- [ ] This PR does **not** weaken the WebSocket origin check or otherwise expand the network attack surface (or it does, and I've explained why below)

## Notes for reviewers

<!-- Anything the reviewer should pay special attention to. -->
