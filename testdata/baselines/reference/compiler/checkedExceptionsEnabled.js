//// [tests/cases/compiler/checkedExceptionsEnabled.ts] ////

//// [checkedExceptionsEnabled.ts]
class IOError extends Error {}

declare function readFile(path: string): string throws IOError;

// Enabled checked exceptions are always build-blocking and fail closed.
function unhandled(): string throws never {
    return readFile("x");
}

readFile("top");


//// [checkedExceptionsEnabled.js]
"use strict";
class IOError extends Error {
}
// Enabled checked exceptions are always build-blocking and fail closed.
function unhandled() {
    return readFile("x");
}
readFile("top");
