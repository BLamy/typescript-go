//go:build js && wasm

// Command tsgoplayground runs the tsgo LSP server inside a browser (or Node)
// WebAssembly host for the checked-exceptions playground.
//
// It exposes a single global function:
//
//	tsgoLspStart(tsconfigJSON, initialFileText, onMessage) => send
//
// where onMessage is called with each server-to-client LSP message as a JSON
// string, and the returned send function delivers client-to-server LSP
// messages, also as JSON strings. Messages are whole JSON-RPC payloads; no
// Content-Length framing is used on either side.
//
// The server runs against an in-memory file system containing a single
// project: /project/tsconfig.json and /project/main.ts, with the bundled
// standard libraries. Everything else — sessions, snapshots, diagnostics
// publishing, hover, completions — is the regular LSP server, unmodified.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/lsp"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

// jsReader delivers messages pushed from JavaScript to the server's read loop.
type jsReader struct {
	ch chan []byte
}

func (r jsReader) Read() (*lsproto.Message, error) {
	data := <-r.ch
	msg := &lsproto.Message{}
	if err := msg.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("playground: malformed LSP message: %w", err)
	}
	return msg, nil
}

// jsWriter forwards server messages to the JavaScript onMessage callback.
type jsWriter struct {
	onMessage js.Value
}

func (w jsWriter) Write(msg *lsproto.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	w.onMessage.Invoke(string(data))
	return nil
}

// consoleWriter sends the server's stderr stream to console.debug.
type consoleWriter struct{}

func (consoleWriter) Write(p []byte) (int, error) {
	js.Global().Get("console").Call("debug", string(p))
	return len(p), nil
}

func start(_ js.Value, args []js.Value) any {
	if len(args) != 3 {
		panic("tsgoLspStart(tsconfigJSON, initialFileText, onMessage)")
	}
	tsconfig := args[0].String()
	initialText := args[1].String()
	onMessage := args[2]

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/project/tsconfig.json": tsconfig,
		"/project/main.ts":       initialText,
	}, true /*useCaseSensitiveFileNames*/))

	in := jsReader{ch: make(chan []byte, 1024)}
	server := lsp.NewServer(&lsp.ServerOptions{
		In:                 in,
		Out:                jsWriter{onMessage: onMessage},
		Err:                consoleWriter{},
		Cwd:                "/project",
		FS:                 fs,
		DefaultLibraryPath: bundled.LibPath(),
	})

	go func() {
		if err := server.Run(context.Background()); err != nil {
			js.Global().Get("console").Call("error", "tsgo LSP server exited: "+err.Error())
		}
	}()

	return js.FuncOf(func(_ js.Value, sendArgs []js.Value) any {
		select {
		case in.ch <- []byte(sendArgs[0].String()):
		default:
			js.Global().Get("console").Call("error", "tsgo LSP inbound queue full; message dropped")
		}
		return nil
	})
}

func main() {
	js.Global().Set("tsgoLspStart", js.FuncOf(start))
	// Signal readiness to the host and park the main goroutine; the server
	// lives in background goroutines for the lifetime of the page.
	if ready := js.Global().Get("tsgoLspReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
