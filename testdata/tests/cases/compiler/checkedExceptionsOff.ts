// @checkedExceptions: false
// @strict: true
// @declaration: true

// With checkedExceptions disabled, `throws` clauses parse, appear in declaration
// output, erase from JS output, and produce no diagnostics; catch variables
// stay unknown.

class IOError extends Error { readonly tag = "io" as const; }

export function readFile(path: string): string throws IOError {
    if (!path) throw new IOError("missing");
    return path;
}

export const parse = (s: string): number throws IOError => s.length;

export interface Api {
    fetchUser(id: string): string throws IOError;
}

export type Fn = (x: string) => number throws IOError;

// No enforcement in any position.
export function unannotated(): string {
    return readFile("x");
}
readFile("top");

export function catchStaysUnknown() {
    try {
        return readFile("x");
    } catch (e) {
        e; // still unknown
        return "";
    }
}
