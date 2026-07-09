//// [tests/cases/compiler/checkedExceptionsTypedCatch.ts] ////

//// [checkedExceptionsTypedCatch.ts]
class IOError extends Error { readonly tag = "io" as const; }
class ParseError extends Error { readonly tag = "parse" as const; }

declare function readFile(path: string): string throws IOError;
declare function parse(text: string): number throws ParseError;

function combined() {
    try {
        return parse(readFile("a"));
    } catch (e) {
        // e: IOError | ParseError
        switch (e.tag) {
            case "io":
                return e.message.length;
            case "parse":
                return -1;
        }
    }
}

function throwsOnly() {
    try {
        throw new ParseError("boom");
    } catch (e) {
        // e: ParseError, from the throw statement's type
        const tag: "parse" = e.tag;
        return tag;
    }
}

function rethrowNarrowed(): void throws ParseError {
    try {
        parse("x");
    } catch (e) {
        // e: ParseError — rethrowing is covered by the clause
        throw e;
    }
}

function nothingTracked() {
    try {
        return JSON.stringify({});
    } catch (e) {
        // e stays unknown: nothing tracked was raised
        return e;
    }
}

function explicitAnnotationWins() {
    try {
        return readFile("a");
    } catch (e: unknown) {
        // explicit annotation is respected
        return e;
    }
}

function innerTryDischarges() {
    try {
        try {
            readFile("a");
        } catch {
            throw new ParseError("converted");
        }
    } catch (e) {
        // e: ParseError — the inner catch discharged IOError but raises ParseError
        const tag: "parse" = e.tag;
        return tag;
    }
}


//// [checkedExceptionsTypedCatch.js]
"use strict";
class IOError extends Error {
    tag = "io";
}
class ParseError extends Error {
    tag = "parse";
}
function combined() {
    try {
        return parse(readFile("a"));
    }
    catch (e) {
        // e: IOError | ParseError
        switch (e.tag) {
            case "io":
                return e.message.length;
            case "parse":
                return -1;
        }
    }
}
function throwsOnly() {
    try {
        throw new ParseError("boom");
    }
    catch (e) {
        // e: ParseError, from the throw statement's type
        const tag = e.tag;
        return tag;
    }
}
function rethrowNarrowed() {
    try {
        parse("x");
    }
    catch (e) {
        // e: ParseError — rethrowing is covered by the clause
        throw e;
    }
}
function nothingTracked() {
    try {
        return JSON.stringify({});
    }
    catch (e) {
        // e stays unknown: nothing tracked was raised
        return e;
    }
}
function explicitAnnotationWins() {
    try {
        return readFile("a");
    }
    catch (e) {
        // explicit annotation is respected
        return e;
    }
}
function innerTryDischarges() {
    try {
        try {
            readFile("a");
        }
        catch {
            throw new ParseError("converted");
        }
    }
    catch (e) {
        // e: ParseError — the inner catch discharged IOError but raises ParseError
        const tag = e.tag;
        return tag;
    }
}
