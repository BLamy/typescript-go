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
    } catch (e) {
        // The later rejection is not a value this synchronous catch can see.
        const notTheRejection: "async" = e;
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

// Returning a promise from a synchronous try does not make its later rejection
// catchable by that try's catch clause.
async function returnedInsideSynchronousTry(): Promise<void> throws never {
    try {
        return rejects();
    } catch {
    }
}

// An explicit Promise constituent can hide thenable timing. A synchronous catch
// cannot discharge a declared effect that may arrive as a later rejection.
declare function maybeAsync(): Promise<void> | void throws "maybe async";
function ambiguousTimingInSynchronousTry(): void throws never {
    try {
        maybeAsync();
    } catch {
    }
}

// Broad legacy returns stay synchronous at their call site, so ordinary
// declaration APIs remain catchable. They become unknown only if an enclosing
// async/Promise-returning function actually assimilates the returned value.
declare function legacyBroadValue(): any;
function catchesLegacyBroadValue(): void throws never {
    try {
        legacyBroadValue();
    } catch {
    }
}
async function assimilatesLegacyBroadValue(): Promise<void> throws never {
    return legacyBroadValue();
}

// Existing Promise values have no tracked rejection slot yet, so returning
// one cannot manufacture a precise or empty rejection effect.
declare const ambientPromiseValue: Promise<void>;
async function returnedPromiseValue(): Promise<void> throws never {
    return ambientPromiseValue;
}
const conciseReturnedPromiseValue = (): Promise<void> throws never => ambientPromiseValue;

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
