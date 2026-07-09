//// [tests/cases/compiler/checkedExceptionsStrictSoundness.ts] ////

//// [checkedExceptionsStrictSoundness.ts]
declare function legacy(): void;
declare function tracked(): void throws "tracked";
declare function explicitlyUnknown(): void throws unknown;
declare function explicitlyAny(): void throws any;

// Ambient declarations without throws metadata are unknown, not throw-free.
function ambientInNever(): void throws never {
    legacy();
}

// Explicit unknown and any effects cannot escape through `throws never`.
function unknownInNever(): void throws never {
    explicitlyUnknown();
}
function anyInNever(): void throws never {
    explicitlyAny();
}

// Unknown effects may be caught or propagated honestly.
function catchesLegacy(): void throws never {
    try {
        legacy();
    } catch (e) {
        const value: unknown = e;
    }
}
function propagatesLegacy(): void throws unknown {
    legacy();
}

// A missing callback effect makes the higher-order function unknown rather
// than silently losing an exception raised by the callback.
function invoke(callback: () => void): void {
    callback();
}
function higherOrderInNever(): void throws never {
    invoke(() => { throw "callback"; });
}

// Structural property access may invoke a getter or Proxy trap.
const accessor = {
    get value(): number {
        throw "getter";
    },
};
function accessorInNever(): number throws never {
    return accessor.value;
}

// One unknown source poisons catch precision instead of allowing an optimistic
// tracked-only type.
function mixedCatch(): void throws never {
    try {
        tracked();
        legacy();
    } catch (e) {
        const unsound: "tracked" = e;
    }
}

// Function types without a clause describe an unknown effect in strict mode.
type LegacyCallback = () => void;
type SafeCallback = () => void throws never;
declare const legacyCallback: LegacyCallback;
const incorrectlyNarrowed: SafeCallback = legacyCallback;

// Unknown effects at top level must be handled.
legacy();
try { legacy(); } catch {}

// Type assertions cannot manufacture a non-throwing callable type.
function assertionSource(): void throws "assertion" {
    throw "assertion";
}
const assertionErasesEffect = assertionSource as () => void throws never;
function callAssertedFunction(): void throws never {
    assertionErasesEffect();
}

// An overload cannot advertise a narrower effect than its implementation.
function lyingOverload(): void throws never;
function lyingOverload(): void {
    throw "implementation";
}
function callLyingOverload(): void throws never {
    lyingOverload();
}


//// [checkedExceptionsStrictSoundness.js]
"use strict";
// Ambient declarations without throws metadata are unknown, not throw-free.
function ambientInNever() {
    legacy();
}
// Explicit unknown and any effects cannot escape through `throws never`.
function unknownInNever() {
    explicitlyUnknown();
}
function anyInNever() {
    explicitlyAny();
}
// Unknown effects may be caught or propagated honestly.
function catchesLegacy() {
    try {
        legacy();
    }
    catch (e) {
        const value = e;
    }
}
function propagatesLegacy() {
    legacy();
}
// A missing callback effect makes the higher-order function unknown rather
// than silently losing an exception raised by the callback.
function invoke(callback) {
    callback();
}
function higherOrderInNever() {
    invoke(() => { throw "callback"; });
}
// Structural property access may invoke a getter or Proxy trap.
const accessor = {
    get value() {
        throw "getter";
    },
};
function accessorInNever() {
    return accessor.value;
}
// One unknown source poisons catch precision instead of allowing an optimistic
// tracked-only type.
function mixedCatch() {
    try {
        tracked();
        legacy();
    }
    catch (e) {
        const unsound = e;
    }
}
const incorrectlyNarrowed = legacyCallback;
// Unknown effects at top level must be handled.
legacy();
try {
    legacy();
}
catch { }
// Type assertions cannot manufacture a non-throwing callable type.
function assertionSource() {
    throw "assertion";
}
const assertionErasesEffect = assertionSource;
function callAssertedFunction() {
    assertionErasesEffect();
}
function lyingOverload() {
    throw "implementation";
}
function callLyingOverload() {
    lyingOverload();
}
