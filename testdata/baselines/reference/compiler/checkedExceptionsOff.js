//// [tests/cases/compiler/checkedExceptionsOff.ts] ////

//// [checkedExceptionsOff.ts]
// With checkedExceptions unset, `throws` clauses parse, appear in declaration
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


//// [checkedExceptionsOff.js]
// With checkedExceptions unset, `throws` clauses parse, appear in declaration
// output, erase from JS output, and produce no diagnostics; catch variables
// stay unknown.
class IOError extends Error {
    tag = "io";
}
export function readFile(path) {
    if (!path)
        throw new IOError("missing");
    return path;
}
export const parse = (s) => s.length;
// No enforcement in any position.
export function unannotated() {
    return readFile("x");
}
readFile("top");
export function catchStaysUnknown() {
    try {
        return readFile("x");
    }
    catch (e) {
        e; // still unknown
        return "";
    }
}


//// [checkedExceptionsOff.d.ts]
declare class IOError extends Error {
    readonly tag: "io";
}
export declare function readFile(path: string): string throws IOError;
export declare const parse: (s: string) => number throws IOError;
export interface Api {
    fetchUser(id: string): string throws IOError;
}
export type Fn = (x: string) => number throws IOError;
export declare function unannotated(): string;
export declare function catchStaysUnknown(): string;
export {};
