# The New Intelligencer

A daily newspaper generated from your Bluesky timeline.

![The New Intelligencer](screenshot.png)

The New Intelligencer transforms your Bluesky feed into a curated newspaper-style digest. It fetches posts from your timeline, groups related posts into stories, writes headlines, and compiles the result either as a browsable HTML page or a portable markdown edition.

## Quick Start

Claude mode:

```bash
./run.sh
```

This runs the original Claude-driven workflow and generates `digest.html` in today's workspace folder.

Ollama mode:

```bash
DIGEST_MODE=ollama OLLAMA_MODEL=llama3.1:8b ./run.sh
```

This runs a resumable local pipeline and generates `digest.md` in today's workspace folder.

## Setup

### Prerequisites

- Go 1.24+
- Go 1.24+
- Bluesky account with an app password

For Claude mode:

- [Claude Code](https://github.com/anthropics/claude-code)

For Ollama mode:

- [Ollama](https://ollama.com/)
- A local model pulled into Ollama, for example `ollama pull llama3.1:8b`

### Bluesky Credentials

1. In Bluesky, go to **Settings > Privacy and Security > App Passwords**
2. Create a new app password
3. Either export them directly:

```bash
export BSKY_HANDLE="your.handle.bsky.social"
export BSKY_PASSWORD="xxxx-xxxx-xxxx-xxxx"
```

4. Or put them in a local `.env` file:

```bash
BSKY_HANDLE=your.handle.bsky.social
BSKY_PASSWORD=xxxx-xxxx-xxxx-xxxx
BSKY_PDS_HOST=https://bsky.social
```

5. Or, on macOS, store them in Keychain:

```bash
security add-generic-password -s "bsky-agent" -a "handle" -w "your.handle.bsky.social"
security add-generic-password -s "bsky-agent" -a "password" -w "xxxx-xxxx-xxxx-xxxx"
```

### Build

```bash
make build
```

## How It Works

The digest is created through a six-stage pipeline:

```
FETCH → CATEGORIZE → CONSOLIDATE → FRONT PAGE → HEADLINES → COMPILE
```

Claude mode uses four Claude Code agents:

| Agent | Role |
|-------|------|
| `bsky-section-categorizer` | Assigns posts to newspaper sections |
| `bsky-consolidator` | Groups posts about the same story together |
| `bsky-front-page-selector` | Picks the top stories for the front page |
| `bsky-headline-editor` | Writes headlines and sets story priorities |

Ollama mode runs the same editorial stages inside the `digest` binary as a slower, sequential overnight job:

```bash
./bin/digest overnight --provider ollama --output markdown
```

That path is designed for a VPS or local box where the whole run can happen in one long-lived process and resume from the workspace if interrupted. It is quality-first by default: if an Ollama call times out or fails, the run stops instead of silently switching to heuristic output.

Each Ollama call is logged into `WORKSPACE/ollama-traces/` as structured JSON, including the prompt, schema, raw response, parsed payload, duration, and any error.

For smoke tests, you can opt into best-effort fallback mode:

```bash
./bin/digest overnight --provider ollama --output markdown --allow-fallbacks
```

If your local model is extremely slow, raise the per-request timeout:

```bash
./bin/digest overnight --provider ollama --output markdown --ollama-timeout-seconds 1800
```

### CLI Commands

```bash
./bin/digest init      # Initialize today's workspace
./bin/digest fetch     # Fetch posts from your timeline
./bin/digest status    # Check workflow progress
./bin/digest compile   # Generate the final digest (HTML by default)
./bin/digest overnight # Run the local Ollama pipeline end-to-end
```

## Development

```bash
make build   # Build the binary
make test    # Run tests
make clean   # Clean build artifacts
```
