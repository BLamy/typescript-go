// @checkedExceptions: error
// @strict: true
// @declaration: true

class IOError extends Error { readonly tag = "io" as const; }
class ParseError extends Error { readonly tag = "parse" as const; }

declare function readFile(path: string): string throws IOError;
declare function parseConfig(text: string): number throws ParseError;

// Unannotated: its throws type is inferred (IOError | ParseError). Signatures
// rebuilt from types — hover/quickinfo, and declaration emit for bindings
// without their own explicit type annotation — now surface it, not just an
// explicit clause.
export function loadConfig(path: string) {
    return parseConfig(readFile(path));
}

// Same, via an arrow assigned to a variable with no explicit type annotation:
// the const's type must be synthesized, so this exercises the same node
// builder path used by hover.
export const loadConfigArrow = (path: string) => parseConfig(readFile(path));

// A function that infers to `never` (no tracked raises) gets no synthesized
// clause at all — consistent with `throws never` not being the same as "no
// clause", but there is nothing to show either way.
export function safe(x: number) {
    return x + 1;
}
