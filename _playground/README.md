# Checked-exceptions playground

A browser playground for the [checked exceptions](../docs/proposals/checked-exceptions.md)
feature. The **real tsgo LSP server** — sessions, snapshots, diagnostics, hover,
completions — is compiled to WebAssembly and runs entirely in the page; Monaco
talks to it over LSP JSON-RPC (whole JSON payloads, no Content-Length framing).
Nothing leaves the browser.

## Layout

- `../cmd/tsgoplayground` — the wasm entry point (`js && wasm` build tags). It
  boots `lsp.NewServer` against an in-memory file system containing
  `/project/tsconfig.json` + `/project/main.ts` and the bundled standard
  libraries, and bridges the server's message streams to JavaScript callbacks
  via a global `tsgoLspStart(tsconfigJSON, initialText, onMessage) => send`.
- `web/` — the static site: Monaco (from CDN) plus a small hand-rolled LSP
  client (`playground.js`). Diagnostics use the pull model
  (`textDocument/diagnostic`); hover and completions are forwarded to the
  server. The `checkedExceptions` selector reloads the page with checking on or
  off in the generated tsconfig.
- `smoke/smoke.cjs` — headless end-to-end test: boots the wasm module under
  Node, performs the LSP handshake, and asserts that checked-exceptions
  diagnostics (TS100021), typed-catch hover, and `throws`-clause hover all
  work. CI runs this before every deploy.
- `playground.workflow.yml` — a GitHub Actions workflow that builds the wasm,
  runs the smoke test, and deploys to GitHub Pages. The automation that opened
  this PR cannot create files under `.github/workflows/`, so enable it with:

  ```sh
  cp _playground/playground.workflow.yml .github/workflows/playground.yml
  ```

## Local development

```sh
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o /tmp/site/playground.wasm ./cmd/tsgoplayground
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" /tmp/site/
cp _playground/web/* /tmp/site/
node _playground/smoke/smoke.cjs /tmp/site/playground.wasm /tmp/site/wasm_exec.js  # verify
python3 -m http.server -d /tmp/site 8080                                           # then open :8080
```

## Enabling GitHub Pages

In the repository settings, set **Pages → Source → GitHub Actions**. The
workflow deploys on pushes to `main` (and the feature branches) or manually via
*Run workflow*.
