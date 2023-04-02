# gh-serve

Serve GitHub Actions HTML artifacts locally 🌍

**gh-serve** is a [GitHub CLI](https://cli.github.com) extension that finds GitHub Actions artifacts for the current PR, or branch, and serves them on a local web server.
This allows you to quickly preview HTML artifacts, such as CI test reports or documentation websites.

## Installation

```
gh extension install ConorMacBride/gh-serve
```

Upgrade:

```
gh extension upgrade gh-serve
```

## Usage

Navigate to a GitHub repository, and run:

```
gh serve
```

It will find artifacts for GitHub Actions workflow runs for the latest commit to the current open PR, or branch.
If there are multiple artifacts, an interactive menu will appear, allowing you to select the one to serve.
The artifact will be hosted at `http://localhost:8080/`.

Available flags:

```text
      --no-browser    don't open the artifact in your default browser
      --no-cache      don't use cached artifacts
      --port string   port to serve the artifact on (default "8080")
  -h, --help          show help for command
```

## How it works

1. 🔎 Finds GitHub Actions workflow runs for the current PR, or branch
   - Only finds runs for the *latest* remote commit
   - If the current branch has an open PR, prefer `pull_request` events
2. 📝 Lists all the artifacts for the workflow runs
   - Interactively select the artifact to serve, if there are multiple
3. 💾 Downloads the artifact
   - Caches in `<repo-root>/.cache/gh-serve/<run-id>/<artifact-name>/`
   - Ignore cache with `--no-cache`
4. 🌍 Serves the artifact on a local web server
   - Default port is `8080`
   - Change port with `--port <port>`
5. 👀 Opens the artifact in your default browser
   - Skip with `--no-browser`
