// @checkedExceptions: warning
// @strict: true

class IOError extends Error {}

declare function readFile(path: string): string throws IOError;

// Reported as a warning, not an error.
function unhandled(): string throws never {
    return readFile("x");
}

readFile("top");
