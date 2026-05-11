# PTY-Backed Stdio Leases

`me run --stdio` runs the wrapped command under a pseudo-terminal instead of plain pipes. Stdio leases are meant to protect interactive Personal Server work, and tools such as Codex, Claude Code, shells, prompts, raw-mode programs, color output, full-screen terminal applications, and window resize behavior require terminal semantics that pipes do not provide. The first implementation targets Unix-like Personal Server workflows with `github.com/creack/pty`; unsupported platforms should fail clearly rather than silently degrading to pipe behavior.
