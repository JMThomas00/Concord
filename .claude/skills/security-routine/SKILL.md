---
name: security-routine
description: Concord's security routine. Use for the end-of-sprint bug and security review (layer 2) or the full audit before a minor release, v0.x.0 (layer 3), or when asked for a security audit, threat model, threat scan or "security pass" on Concord itself. Not for plugins built with the SDK (that's the concord skill).
---

# Concord security routine

Agreed with Jordan 2026-11-05, after the first full audit (vault: "Security Audit 2026-11-05"). Three layers:

1. **Every change (automatic):** `.github/workflows/checks.yml`: build, vet, tests, `-race`, govulncheck, gosec (high/high, reviewed false positives carry `#nosec Gxxx -- why`). Weekly too, for newly published vulnerabilities.
2. **Every sprint (this skill, "sprint review"):** about an hour, on the sprint's diff.
3. **Before every minor release (this skill, "release audit"):** the whole codebase, with what changed since the last audit looked at hardest. Patch releases get layer 2 only, unless they touch security.

Either way the result is a vault note in `10 Projects/Concord/Tasks/` (`Security Review <sprint or version> <date>.md` or `Security Audit <date>.md`): findings worst first with file:line and a fix, **what held up**, **not covered**, and later each fix's commit. Pull the vault before, commit as `Claude <noreply@anthropic.com>`, push after. Report to Jordan in chat before fixing anything; fix on a branch, one PR.

## Trust boundaries (where to look)

Everything that crosses one of these is attacker-controlled until checked:

- **People → server:** every WebSocket op (`internal/server/client.go` `handleMessage`) and HTTP route (`registerAPIRoutes`). Each needs: membership, the right permission (`checkPermission`, or `hasChannelPermission` for anything per channel), rank (`checkOutranks`), limits (size, count, rate: `ws_limits.go` costs).
- **Server → other people:** anything broadcast. What does each recipient learn (emails, IPs, presence, who's in which channel)? Inspect the actual JSON on the wire in a test, not just the code: the email leak (finding 19) was only visible that way.
- **Server/others → client terminal:** text drawn in the TUI. `Message.StripControl` covers the WebSocket and `decodeClean` the client's HTTP. New paths that bypass `Connection`'s read loop must use them. Plugin frames go through `sanitizePaneFrame`.
- **Client → OS:** links (`safeLink`/`browserCommand`, never a shell), file paths from others (`models.SafeFilename`), anything run (`exec.Command` with fixed programs only), files written in `~/.concord` (0600 in a 0700 folder).
- **Peer to peer:** voice and file signals (only between people in the same call / who can see the channel), IP exposure, Tor mode (no direct connections).
- **Hub:** registrations are untrusted (host, port, listing text); outbound requests only through `publicClient` unless `allow_private_servers`; federation peers.
- **Plugins:** archives (zip-slip, size), manifests, the env allowlist, frames for channels they don't own, client WASM (signature, capabilities, limits).
- **Secrets at rest:** session tokens hashed, the database key, backup key, plugin secrets key, Tor keys: modes, never logged, never in backups unless meant to be.
- **Supply chain:** Go version supported (1.26/1.27 as of 2026-11), dependencies (govulncheck), release checksums (signed releases are still open: finding 18).

## Sprint review (layer 2)

1. `git log --oneline <last review's commit>..main` and the diff. List each new feature or changed path.
2. **Threat-model each new feature** with STRIDE, at each boundary above it touches: who could spoof, tamper, repudiate, read, deny service, or gain rights? Write one line per threat that applies and how the code stops it (or doesn't).
3. **Checklist on the diff:**
   - New op or route: membership, permission (overwrite-aware?), rank, size/count limits, its cost in `ws_limits.go`?
   - New broadcast: does every recipient need every field? No emails, IPs, tokens, other people's private state.
   - New text from others drawn on screen: does it come through `Connection` (stripped) or another way?
   - Files, paths, links, `exec`: from someone else? Cleaned?
   - New secret or file on disk: mode, location, logged anywhere, in backups?
   - New dependency or version bump: govulncheck, and the UI pinning rule (CLAUDE.md, Plugin SDK).
   - Docs: does `docs/what-concord-protects.md` stay true?
4. Run what CI can't: `CONCORD_TOR_E2E=1` Tor tests if Tor changed, `TestRealInstall` if the installer changed, a real client against VM 113 (The Vineyard) for anything user-facing.
5. Bugs found that aren't security go in the same note, separately.

## Release audit (layer 3)

Everything in the sprint review, for the whole codebase, plus:

1. **Tools:** `govulncheck -tags novoice ./...` (and the voice client without the tag, and both SDK modules), `gosec` at all severities (triage, don't fix the noise: weak random for visuals, int conversions), `go test -race` on everything, `go list -m -u all` for stale dependencies, the Go version's support status.
2. **Every WebSocket op and HTTP route**, one by one, against the trust-boundary checks. Keep the table in the note.
3. **A hostile server's view:** what can a malicious server do to a client (escapes, paths, huge payloads, fake signals)? And **a hostile member's view** of the server.
4. **Wire inspection:** dump `READY`, `SERVER_CREATE`, a message, a whisper, a presence update for a non-admin and read every field.
5. **What wasn't covered last time:** start there. After 2026-11-05: the install scripts and the installer's privilege use, the plugin SDK, the WASM sandbox, a hostile server beyond escapes, and fuzzing (`go test -fuzz` on `StripControlJSON`, `SafeFilename`, the TOML/manifest parsers, invite parsing).
6. Each fix gets a test that fails without it (run it both ways), and the vault note gets the commit.

## Standards behind this

STRIDE for threat modeling; OWASP ASVS for what to check in a networked app; NIST SSDF (SP 800-218) and OWASP SAMM for the process. None of this is an independent audit: `docs/what-concord-protects.md` says Concord hasn't had one, and that stays true until someone outside does it.
