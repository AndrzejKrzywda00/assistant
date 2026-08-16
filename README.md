# Assistant

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A local, keyboard-first productivity app for the terminal. It stores tasks in a
human-readable JSON file and exposes both an interactive interface and a
scriptable CLI, so people and coding agents can safely work with the same list.

Interactive mode always opens with a four-digit PIN screen. On first launch,
enter your name, then create and confirm a new PIN; later launches greet you by
name and require the PIN before showing the
outline dashboard. The salted PIN verifier is stored beside the task data file.

There is no built-in project. The first task asks you to create its project.
Afterward, `a` creates tasks directly inside the currently selected project;
use `p` when you want to create another project.

## Install on macOS

Install the prebuilt release with Homebrew:

```sh
brew install AndrzejKrzywda00/tap/assistant
```

Upgrade later releases with `brew upgrade assistant`.

Go 1.23 or newer is required. From this checkout, install it with:

```sh
go install ./cmd/assistant
```

Or install the latest published version directly from GitHub:

```sh
go install github.com/AndrzejKrzywda00/assistant/cmd/assistant@latest
```

Tagged versions are also available as prebuilt macOS and Linux archives on the
[GitHub Releases](https://github.com/AndrzejKrzywda00/assistant/releases) page.

Ensure `$(go env GOPATH)/bin` is on `PATH`, then run `assistant`.

## Keyboard controls

| Key | Action |
| --- | --- |
| `a` | Add a task |
| `p` | Create a project |
| `h` / left arrow | Focus the Spaces and Projects sidebar |
| `l` / right arrow, `enter` | Return to the selected space's tasks |
| `j` / `k`, arrows | Select Today, Blocked, Waiting, a project, or a task |
| `m` | Move a task through Today, Blocked, and Waiting |
| `space` | Complete or reopen the selected task; it stays visible |
| `enter` | Edit the selected task |
| `f` | Replace the task pane with the selected task's Focus tab |
| `p` in Focus | Set explicit task progress from 0–100% |
| `escape` in Focus | Return to Outline |

Task progress is represented by the circular marker itself: `○` begins empty,
intermediate circles fill as work advances, and `●` is fully complete.
| `d` | Delete the selected task (with confirmation) |
| `t` | Toggle open/all tasks |
| `g` / `G` | Jump to first / last task |
| `?` | Show help |
| `q` | Quit |

The TUI uses the alternate screen and restores the terminal when it exits.

## Claude Code and automation

Claude Code terminals can call the same executable without opening the TUI:

```sh
assistant project add "Authentication"
assistant add --project "Authentication" "Investigate failing test"
assistant list --project "Authentication" --json
assistant done 1
assistant context
```

`list --json` is the stable machine-readable interface. `context` emits compact
Markdown suitable for adding to a Claude prompt. Updates use a cross-process
file lock and atomic replacement, so multiple local terminals do not overwrite
one another.

For a project-specific list, set the path when launching Claude Code and the TUI:

```sh
export ASSISTANT_DATA_PATH="$PWD/.assistant/tasks.json"
claude
```

By default macOS stores data at
`~/Library/Application Support/assistant/tasks.json`. Run `assistant path` to
print the exact active location. The task file and lock are created with user-only
permissions. No server, account, or network connection is used.

## CLI

```text
assistant                         Open the interactive terminal UI
assistant add [--project NAME] <title>  Add a task to a project
assistant list [--json] [--all] [--project NAME]
assistant projects                    List projects
assistant project add <name>          Create a project
assistant done <id>               Complete a task
assistant reopen <id>             Reopen a task
assistant delete <id>             Delete a task
assistant path                    Print the local data file path
assistant context                 Print a Claude-friendly task summary
assistant version                 Print version and build information
```

## License

Assistant is available under the [MIT License](LICENSE).

## Releasing

Pushing a semantic version tag runs the release workflow, which tests the code,
builds macOS and Linux archives, generates SHA-256 checksums, and publishes a
GitHub Release:

```sh
git tag v0.1.0
git push origin v0.1.0
```

### One-time Homebrew setup

1. Create a public repository named `AndrzejKrzywda00/homebrew-tap` with a
   `Formula` directory.
2. Create a fine-grained personal access token with access only to that
   repository and `Contents: Read and write` permission.
3. In this repository, add the token under **Settings → Secrets and variables →
   Actions** as `HOMEBREW_TAP_TOKEN`.

Each subsequent release generates `Formula/assistant.rb` from the release
checksums and commits it to the tap automatically.
