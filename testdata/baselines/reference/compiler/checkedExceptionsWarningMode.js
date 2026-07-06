//// [tests/cases/compiler/checkedExceptionsWarningMode.ts] ////

//// [checkedExceptionsWarningMode.ts]
class IOError extends Error {}

declare function readFile(path: string): string throws IOError;

// Reported as a warning, not an error.
function unhandled(): string throws never {
    return readFile("x");
}

readFile("top");


//// [checkedExceptionsWarningMode.js]
"use strict";
class IOError extends Error {
}
// Reported as a warning, not an error.
function unhandled() {
    return readFile("x");
}
readFile("top");
