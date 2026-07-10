// @checkedExceptions: true
// @strict: true
// @target: es2022

async function rejects(): Promise<void> throws "async" {
    throw "async";
}

// A synchronous try/catch does not handle a promise rejection.
function fakeSynchronousHandling(): void throws never {
    try {
        rejects();
    } catch {
    }
}

// Await transfers the rejection effect into the surrounding control flow.
async function unhandledAwait(): Promise<void> throws never {
    await rejects();
}

// Awaiting inside try/catch discharges the rejection.
async function handledAwait(): Promise<void> throws never {
    try {
        await rejects();
    } catch (e) {
        const effect: "async" = e;
    }
}

// Directly returning a promise propagates its rejection effect through the
// enclosing function's clause.
async function returnedPromise(): Promise<void> throws "async" {
    return rejects();
}

// A floating ambient promise is unknown even inside synchronous try/catch.
declare function legacyAsync(): Promise<void>;
function floatingLegacyPromise(): void throws never {
    try {
        legacyAsync();
    } catch {
    }
}

// Awaiting an ambient promise is catchable, but its catch value is unknown.
async function handledLegacyAwait(): Promise<void> throws never {
    try {
        await legacyAsync();
    } catch (e) {
        const effect: unknown = e;
    }
}

// Returning an ambient promise requires honest unknown propagation.
async function returnedLegacyPromise(): Promise<void> throws unknown {
    return legacyAsync();
}

// A callback may run after the caller's control-flow boundary has disappeared.
// Until a signature can prove synchronous non-escaping invocation, the
// callback must discharge its own effect.
declare function later(callback: () => void): void throws never;
function callbackThrows(): void throws "later" {
    throw "later";
}
function synchronousCatchCannotHandleCallback(): void throws never {
    try {
        later(callbackThrows);
    } catch {
    }
}

// Handling inside the callback is valid regardless of when it is invoked.
function callbackHandlesItsOwnEffect(): void throws never {
    later(() => {
        try {
            callbackThrows();
        } catch {
        }
    });
}
