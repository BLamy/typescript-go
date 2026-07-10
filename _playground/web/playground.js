// Checked-exceptions playground: Monaco wired to the real tsgo LSP server
// running in WebAssembly. Messages are whole JSON-RPC payloads exchanged as
// strings with the Go side (no Content-Length framing).
"use strict";

const FILE_URI = "file:///project/main.ts";

const SAMPLE = `class IOError extends Error { readonly tag = "io" as const; }
class ParseError extends Error { readonly tag = "parse" as const; }

function readFile(path: string): string throws IOError {
    if (!path) throw new IOError("missing: " + path);
    return "contents of " + path;
}

function parseConfig(text: string): number throws ParseError {
    if (text.length === 0) throw new ParseError("empty config");
    return text.length;
}

// Unannotated functions propagate silently; their throws type is inferred.
function loadConfig(path: string) {
    return parseConfig(readFile(path));
}

// Hover \`e\` below: it is typed IOError | ParseError, not unknown.
function main(): number {
    try {
        return loadConfig("app.json");
    } catch (e) {
        switch (e.tag) {
            case "io": return -1;
            case "parse": return -2;
        }
    }
}

// Error: raises IOError but declares \`throws never\`.
function mustNotThrow(): string throws never {
    return readFile("x.txt");
}

// Error: assignability is covariant in the throws clause.
type SafeFn = () => void throws never;
const unsafe: SafeFn = () => { readFile("y.txt"); };

// Error: code outside any function must catch everything.
loadConfig("boot.json");
`;

const mode = (() => {
    const m = new URLSearchParams(location.search).get("mode");
    return m === "off" ? "off" : "on";
})();

const modeSelect = document.getElementById("mode");
modeSelect.value = mode;
modeSelect.addEventListener("change", () => {
    const params = new URLSearchParams(location.search);
    params.set("mode", modeSelect.value);
    location.search = params.toString();
});

const statusEl = document.getElementById("status");
const statusText = document.getElementById("status-text");
function setStatus(kind, text) {
    statusEl.className = kind;
    statusText.textContent = text;
}

function tsconfig() {
    const options = { strict: true, noEmit: true, module: "esnext", target: "esnext" };
    if (mode !== "off") options.checkedExceptions = true;
    return JSON.stringify({ compilerOptions: options });
}

// ── Minimal LSP client ─────────────────────────────────────────────────────

class LspClient {
    constructor(send) {
        this.send = send;
        this.nextId = 1;
        this.pending = new Map();
        this.notificationHandlers = new Map();
    }

    request(method, params) {
        const id = this.nextId++;
        return new Promise((resolve, reject) => {
            this.pending.set(id, { resolve, reject });
            this.send(JSON.stringify({ jsonrpc: "2.0", id, method, params }));
        });
    }

    notify(method, params) {
        this.send(JSON.stringify({ jsonrpc: "2.0", method, params }));
    }

    respond(id, result) {
        this.send(JSON.stringify({ jsonrpc: "2.0", id, result }));
    }

    onNotification(method, handler) {
        this.notificationHandlers.set(method, handler);
    }

    handle(raw) {
        const msg = JSON.parse(raw);
        if (msg.id !== undefined && msg.method !== undefined) {
            // Server-to-client request.
            switch (msg.method) {
                case "workspace/configuration":
                    this.respond(msg.id, (msg.params?.items ?? []).map(() => null));
                    break;
                case "client/registerCapability":
                case "client/unregisterCapability":
                case "window/workDoneProgress/create":
                default:
                    this.respond(msg.id, null);
            }
        } else if (msg.id !== undefined) {
            // Response to one of our requests.
            const pending = this.pending.get(msg.id);
            if (pending) {
                this.pending.delete(msg.id);
                if (msg.error) pending.reject(new Error(msg.error.message));
                else pending.resolve(msg.result);
            }
        } else if (msg.method) {
            const handler = this.notificationHandlers.get(msg.method);
            if (handler) handler(msg.params);
            else if (msg.method === "window/logMessage") console.debug("[tsgo]", msg.params?.message);
        }
    }
}

// ── Boot ───────────────────────────────────────────────────────────────────

async function startWasm(onMessage) {
    const go = new Go();
    setStatus("", "Downloading compiler…");
    const wasmReady = new Promise(resolve => { globalThis.tsgoLspReady = resolve; });
    let result;
    try {
        result = await WebAssembly.instantiateStreaming(fetch("playground.wasm"), go.importObject);
    } catch {
        // Some servers mislabel the MIME type; fall back to ArrayBuffer.
        const bytes = await (await fetch("playground.wasm")).arrayBuffer();
        result = await WebAssembly.instantiate(bytes, go.importObject);
    }
    setStatus("", "Starting language server…");
    go.run(result.instance); // resolves tsgoLspReady, then parks
    await wasmReady;
    return globalThis.tsgoLspStart(tsconfig(), SAMPLE, onMessage);
}

function loadMonaco() {
    return new Promise(resolve => {
        require.config({ paths: { vs: "https://cdn.jsdelivr.net/npm/monaco-editor@0.52.2/min/vs" } });
        require(["vs/editor/editor.main"], () => resolve(globalThis.monaco));
    });
}

const severityNames = { 1: "error", 2: "warning", 3: "info", 4: "hint" };

async function main() {
    const monaco = await loadMonaco();

    // The tsgo LSP server is the single source of truth. Monaco's built-in
    // TypeScript mode auto-registers its own hover/completion/etc. providers
    // backed by a bundled, unmodified TS engine (it cannot parse `throws`
    // clauses) — left enabled, they stack with ours and every hover/completion
    // shows duplicated, conflicting entries. Disabling diagnostics alone does
    // not stop this; every worker-backed feature must be turned off here so
    // only our LSP-backed providers (registered below) respond.
    monaco.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
        noSemanticValidation: true,
        noSyntaxValidation: true,
        noSuggestionDiagnostics: true,
    });
    monaco.languages.typescript.typescriptDefaults.setModeConfiguration({
        completionItems: false,
        hovers: false,
        documentSymbols: false,
        definitions: false,
        references: false,
        documentHighlights: false,
        rename: false,
        diagnostics: false,
        documentRangeFormattingEdits: false,
        signatureHelp: false,
        onTypeFormattingEdits: false,
        codeActions: false,
        inlayHints: false,
    });

    const editor = monaco.editor.create(document.getElementById("editor"), {
        value: SAMPLE,
        language: "typescript",
        theme: "vs-dark",
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 13,
        fixedOverflowWidgets: true,
    });
    const model = editor.getModel();

    let client;
    const send = await startWasm(raw => client.handle(raw));
    client = new LspClient(send);

    async function refreshDiagnostics() {
        const report = await client.request("textDocument/diagnostic", {
            textDocument: { uri: FILE_URI },
        });
        const diagnostics = report?.items ?? [];
        const markers = diagnostics.map(d => ({
            severity: { 1: 8, 2: 4, 3: 2, 4: 1 }[d.severity ?? 1],
            message: d.message,
            code: d.code !== undefined ? String(d.code) : undefined,
            source: d.source ?? "tsgo",
            startLineNumber: d.range.start.line + 1,
            startColumn: d.range.start.character + 1,
            endLineNumber: d.range.end.line + 1,
            endColumn: d.range.end.character + 1,
        }));
        monaco.editor.setModelMarkers(model, "tsgo", markers);
        renderProblems(diagnostics, editor);
    }

    await client.request("initialize", {
        processId: null,
        rootUri: "file:///project",
        capabilities: {
            textDocument: {
                diagnostic: { dynamicRegistration: false },
                hover: { contentFormat: ["markdown", "plaintext"] },
                completion: { completionItem: { snippetSupport: false } },
            },
            workspace: { configuration: true },
        },
    });
    client.notify("initialized", {});
    client.notify("textDocument/didOpen", {
        textDocument: { uri: FILE_URI, languageId: "typescript", version: 1, text: SAMPLE },
    });
    refreshDiagnostics();

    let version = 1;
    let changeTimer;
    model.onDidChangeContent(() => {
        clearTimeout(changeTimer);
        changeTimer = setTimeout(() => {
            client.notify("textDocument/didChange", {
                textDocument: { uri: FILE_URI, version: ++version },
                contentChanges: [{ text: model.getValue() }],
            });
            refreshDiagnostics();
        }, 150);
    });

    monaco.languages.registerHoverProvider("typescript", {
        async provideHover(_model, position) {
            const result = await client.request("textDocument/hover", {
                textDocument: { uri: FILE_URI },
                position: { line: position.lineNumber - 1, character: position.column - 1 },
            });
            if (!result) return null;
            const value = typeof result.contents === "string" ? result.contents : result.contents.value;
            const hover = { contents: [{ value }] };
            if (result.range) {
                hover.range = new monaco.Range(
                    result.range.start.line + 1,
                    result.range.start.character + 1,
                    result.range.end.line + 1,
                    result.range.end.character + 1,
                );
            }
            return hover;
        },
    });

    monaco.languages.registerCompletionItemProvider("typescript", {
        triggerCharacters: [".", '"', "'", "/", "@", "<"],
        async provideCompletionItems(_model, position) {
            const result = await client.request("textDocument/completion", {
                textDocument: { uri: FILE_URI },
                position: { line: position.lineNumber - 1, character: position.column - 1 },
            });
            const items = (result?.items ?? result ?? []).map(item => ({
                label: item.label,
                // LSP CompletionItemKind and Monaco CompletionItemKind diverge;
                // this coarse mapping covers the common kinds.
                kind: { 2: 0, 3: 1, 4: 2, 5: 3, 6: 4, 7: 5, 8: 7, 10: 9, 13: 12, 14: 17, 21: 14 }[item.kind] ?? 18,
                insertText: item.insertText ?? item.label,
                sortText: item.sortText,
                detail: item.detail,
                range: undefined,
            }));
            return { suggestions: items, incomplete: !!result?.isIncomplete };
        },
    });

    setStatus("ready", `Ready — checkedExceptions: ${mode}`);
}

function renderProblems(diagnostics, editor) {
    const list = document.getElementById("problems-list");
    list.textContent = "";
    if (diagnostics.length === 0) {
        const div = document.createElement("div");
        div.className = "empty";
        div.textContent = "No problems detected.";
        list.appendChild(div);
        return;
    }
    for (const d of diagnostics) {
        const div = document.createElement("div");
        div.className = `problem severity-${d.severity ?? 1}`;
        const loc = document.createElement("span");
        loc.className = "loc";
        loc.textContent = `${d.range.start.line + 1}:${d.range.start.character + 1} `;
        const code = document.createElement("span");
        code.className = "code";
        code.textContent = d.code !== undefined ? `${severityNames[d.severity ?? 1]} TS${d.code} ` : "";
        div.append(loc, code, document.createTextNode(d.message));
        div.addEventListener("click", () => {
            editor.setPosition({ lineNumber: d.range.start.line + 1, column: d.range.start.character + 1 });
            editor.revealLineInCenter(d.range.start.line + 1);
            editor.focus();
        });
        list.appendChild(div);
    }
}

main().catch(err => {
    console.error(err);
    setStatus("error", `Failed to start: ${err.message}`);
});
