// Headless end-to-end test for the playground's wasm LSP server: boots the
// module under Node, speaks LSP to it, and asserts that checked-exceptions
// diagnostics and typed-catch hover both work.
//
// Usage: node smoke.cjs <path-to-playground.wasm> <path-to-wasm_exec.js>
"use strict";

const fs = require("node:fs");
const path = require("node:path");

const [wasmPath, wasmExecPath] = process.argv.slice(2);
if (!wasmPath || !wasmExecPath) {
    console.error("usage: node smoke.cjs <playground.wasm> <wasm_exec.js>");
    process.exit(2);
}
require(path.resolve(wasmExecPath)); // defines globalThis.Go

const SAMPLE = [
    'class IOError extends Error { readonly tag = "io" as const; }',
    "declare function readFile(path: string): string throws IOError;",
    "function mustNotThrow(): void throws never {",
    '    readFile("x");',
    "}",
    "declare function legacy(): void;",
    "function strictMustNotThrow(): void throws never {",
    "    legacy();",
    "}",
    'try { readFile("y"); } catch (e) { e; }',
    "",
].join("\n");

const TSCONFIG = JSON.stringify({
    compilerOptions: { strict: true, noEmit: true, checkedExceptions: "strict" },
});

const FILE_URI = "file:///project/main.ts";

async function main() {
    const deadline = setTimeout(() => {
        console.error("FAIL: timed out after 120s");
        process.exit(1);
    }, 120_000);

    let send;
    let nextId = 1;
    const pending = new Map();

    function onMessage(raw) {
        const msg = JSON.parse(raw);
        if (msg.id !== undefined && msg.method !== undefined) {
            // Server-to-client request: answer with an empty result.
            const result = msg.method === "workspace/configuration"
                ? (msg.params?.items ?? []).map(() => null)
                : null;
            send(JSON.stringify({ jsonrpc: "2.0", id: msg.id, result }));
        } else if (msg.id !== undefined) {
            const p = pending.get(msg.id);
            if (p) {
                pending.delete(msg.id);
                msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result);
            }
        }
    }

    function request(method, params) {
        const id = nextId++;
        return new Promise((resolve, reject) => {
            pending.set(id, { resolve, reject });
            send(JSON.stringify({ jsonrpc: "2.0", id, method, params }));
        });
    }
    function notify(method, params) {
        send(JSON.stringify({ jsonrpc: "2.0", method, params }));
    }

    // Boot the wasm module.
    const go = new Go();
    const ready = new Promise(resolve => { globalThis.tsgoLspReady = resolve; });
    const { instance } = await WebAssembly.instantiate(fs.readFileSync(path.resolve(wasmPath)), go.importObject);
    go.run(instance); // never resolves; the server parks in select{}
    await ready;
    send = globalThis.tsgoLspStart(TSCONFIG, SAMPLE, onMessage);

    await request("initialize", {
        processId: null,
        rootUri: "file:///project",
        capabilities: {
            textDocument: {
                hover: { contentFormat: ["markdown", "plaintext"] },
                diagnostic: { dynamicRegistration: false },
            },
            workspace: { configuration: true },
        },
    });
    notify("initialized", {});
    notify("textDocument/didOpen", {
        textDocument: { uri: FILE_URI, languageId: "typescript", version: 1, text: SAMPLE },
    });

    // 1. Checked-exceptions diagnostics via the pull model (textDocument/diagnostic).
    const report = await request("textDocument/diagnostic", {
        textDocument: { uri: FILE_URI },
    });
    const diagnostics = report?.items ?? [];
    const codes = diagnostics.map(d => String(d.code));
    if (!codes.includes("100021")) {
        console.error("FAIL: expected TS100021 in diagnostics, got:", JSON.stringify(report, null, 2));
        process.exit(1);
    }
    console.log("ok: textDocument/diagnostic reported TS100021 (unhandled IOError inside `throws never`)");

    if (!diagnostics.some(d => String(d.code) === "100021" && d.message.includes("unknown"))) {
        console.error("FAIL: expected strict mode to report an unknown legacy effect, got:", JSON.stringify(report, null, 2));
        process.exit(1);
    }
    console.log("ok: strict mode reports an unannotated ambient call as unknown");

    // 2. The catch variable is typed from the try block's raises.
    const line = SAMPLE.split("\n").findIndex(text => text.startsWith("try {"));
    const character = SAMPLE.split("\n")[line].indexOf("{ e; }") + 2;
    const hover = await request("textDocument/hover", {
        textDocument: { uri: FILE_URI },
        position: { line, character },
    });
    const hoverText = hover && (typeof hover.contents === "string" ? hover.contents : hover.contents.value);
    if (!hoverText || !hoverText.includes("IOError")) {
        console.error("FAIL: expected typed-catch hover to mention IOError, got:", hoverText);
        process.exit(1);
    }
    console.log("ok: hover on catch variable shows IOError");

    // 3. Hover on the declaration shows the throws clause.
    const declarationLine = SAMPLE.split("\n").findIndex(text => text.includes("declare function readFile"));
    const declHover = await request("textDocument/hover", {
        textDocument: { uri: FILE_URI },
        position: { line: declarationLine, character: SAMPLE.split("\n")[declarationLine].indexOf("readFile") + 1 },
    });
    const declText = declHover && (typeof declHover.contents === "string" ? declHover.contents : declHover.contents.value);
    if (!declText || !declText.includes("throws")) {
        console.error("FAIL: expected declaration hover to include the throws clause, got:", declText);
        process.exit(1);
    }
    console.log("ok: declaration hover includes the throws clause");

    clearTimeout(deadline);
    console.log("PASS: wasm LSP smoke test");
    process.exit(0);
}

main().catch(err => {
    console.error("FAIL:", err);
    process.exit(1);
});
