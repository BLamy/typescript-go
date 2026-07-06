//// [tests/cases/compiler/checkedExceptionsDeclareAndPropagate.ts] ////

//// [checkedExceptionsDeclareAndPropagate.ts]
class IOError extends Error { readonly tag = "io" as const; }
class ParseError extends Error { readonly tag = "parse" as const; }

function readFile(path: string): string throws IOError {
    if (!path) throw new IOError("missing");
    return path;
}

// Unannotated functions propagate silently; their throws type is inferred.
function passthrough(): string {
    return readFile("x");
}

// Error: raises IOError but declares `throws never`.
function declaresNever(): string throws never {
    return readFile("x");
}

// Error: IOError is not assignable to ParseError.
function declaresWrongType(): string throws ParseError {
    return readFile("x");
}

// OK: clause is a superset.
function declaresUnion(): string throws IOError | ParseError {
    return readFile("x");
}

// OK: handled by try/catch.
function handles(): string {
    try {
        return readFile("x");
    } catch {
        return "";
    }
}

// Error: thrown type not in the clause.
function throwsUndeclared(): void throws IOError {
    throw new ParseError("nope");
}

// OK: throw matches the clause.
function throwsDeclared(): void throws IOError {
    throw new IOError("yep");
}

// `throws any` opts callers out of enforcement.
function escapeHatch(): void throws any {
    throw new IOError("anything");
}
function callsEscapeHatch(): void {
    escapeHatch();
}

// Raises in a catch or finally block are not discharged by that try.
function raiseInCatch(): void throws never {
    try {
        readFile("a");
    } catch {
        readFile("b"); // error
    } finally {
        readFile("c"); // error
    }
}

// Top-level code must handle everything.
readFile("top"); // error
try { readFile("top"); } catch {} // ok
passthrough(); // error: inferred IOError surfaces here

// Inferred throws propagate through unannotated arrows, including IIFEs.
const arrowInferred = () => { throw new ParseError("x"); };
arrowInferred(); // error
(function () { throw new IOError("iife"); })(); // error


//// [checkedExceptionsDeclareAndPropagate.js]
"use strict";
class IOError extends Error {
    tag = "io";
}
class ParseError extends Error {
    tag = "parse";
}
function readFile(path) {
    if (!path)
        throw new IOError("missing");
    return path;
}
// Unannotated functions propagate silently; their throws type is inferred.
function passthrough() {
    return readFile("x");
}
// Error: raises IOError but declares `throws never`.
function declaresNever() {
    return readFile("x");
}
// Error: IOError is not assignable to ParseError.
function declaresWrongType() {
    return readFile("x");
}
// OK: clause is a superset.
function declaresUnion() {
    return readFile("x");
}
// OK: handled by try/catch.
function handles() {
    try {
        return readFile("x");
    }
    catch {
        return "";
    }
}
// Error: thrown type not in the clause.
function throwsUndeclared() {
    throw new ParseError("nope");
}
// OK: throw matches the clause.
function throwsDeclared() {
    throw new IOError("yep");
}
// `throws any` opts callers out of enforcement.
function escapeHatch() {
    throw new IOError("anything");
}
function callsEscapeHatch() {
    escapeHatch();
}
// Raises in a catch or finally block are not discharged by that try.
function raiseInCatch() {
    try {
        readFile("a");
    }
    catch {
        readFile("b"); // error
    }
    finally {
        readFile("c"); // error
    }
}
// Top-level code must handle everything.
readFile("top"); // error
try {
    readFile("top");
}
catch { } // ok
passthrough(); // error: inferred IOError surfaces here
// Inferred throws propagate through unannotated arrows, including IIFEs.
const arrowInferred = () => { throw new ParseError("x"); };
arrowInferred(); // error
(function () { throw new IOError("iife"); })(); // error


//// [checkedExceptionsDeclareAndPropagate.d.ts]
declare class IOError extends Error {
    readonly tag: "io";
}
declare class ParseError extends Error {
    readonly tag: "parse";
}
declare function readFile(path: string): string throws IOError;
declare function passthrough(): string;
declare function declaresNever(): string throws never;
declare function declaresWrongType(): string throws ParseError;
declare function declaresUnion(): string throws IOError | ParseError;
declare function handles(): string;
declare function throwsUndeclared(): void throws IOError;
declare function throwsDeclared(): void throws IOError;
declare function escapeHatch(): void throws any;
declare function callsEscapeHatch(): void;
declare function raiseInCatch(): void throws never;
declare const arrowInferred: () => never;
