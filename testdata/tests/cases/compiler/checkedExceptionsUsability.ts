// @checkedExceptions: true
// @strict: true

class IOError extends Error {
    readonly tag = "io" as const;
}

class ParseError extends Error {
    readonly tag = "parse" as const;
}

function readFile(path: string): string throws IOError {
    if (!path) throw new IOError("missing: " + path);
    return "contents of " + path;
}

function parseConfig(text: string): number throws ParseError {
    if (text.length === 0) throw new ParseError("empty config");
    return text.length;
}

function loadConfig(path: string) {
    return parseConfig(readFile(path));
}

function main(): number throws never {
    try {
        return loadConfig("app.json");
    } catch (e) {
        if (e.tag === "io") return -1;
        return -2;
    }
}
