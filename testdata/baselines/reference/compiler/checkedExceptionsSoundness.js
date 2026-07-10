//// [tests/cases/compiler/checkedExceptionsSoundness.ts] ////

//// [checkedExceptionsSoundness.ts]
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

// A callback cannot hide inside an option object and escape the caller's
// synchronous exception boundary.
declare function register(options: {
    callback: () => void throws "nested";
}): void throws never;
register({ callback: () => { throw "nested"; } });

// Structural property access may invoke a getter or Proxy trap.
const accessor = {
    get value(): number {
        throw "getter";
    },
};
function accessorInNever(): number throws never {
    return accessor.value;
}

// Getter and setter effects are distinct: reading only executes the getter,
// while assigning executes the setter.
const splitAccessor = {
    get value(): number { return 1; },
    set value(next: number) { throw "setter"; },
};
function readsSafeGetter(): number throws never {
    return splitAccessor.value;
}
function writesThrowingSetter(): void throws never {
    splitAccessor.value = 2;
}

// Structural assignment cannot erase a getter or setter effect by flowing an
// accessor into a concrete data-property contract.
class DataBox { value = 1; }
const getterSource = { get value(): number { throw "erased getter"; } };
const getterErasure: DataBox = getterSource;
const setterSource = {
    get value(): number { return 1; },
    set value(next: number) { throw "erased setter"; },
};
const setterErasure: DataBox = setterSource;
const assertedGetterErasure = getterSource as DataBox;

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

// Function types without a clause describe an unknown effect when enabled.
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


//// [checkedExceptionsSoundness.js]
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
register({ callback: () => { throw "nested"; } });
// Structural property access may invoke a getter or Proxy trap.
const accessor = {
    get value() {
        throw "getter";
    },
};
function accessorInNever() {
    return accessor.value;
}
// Getter and setter effects are distinct: reading only executes the getter,
// while assigning executes the setter.
const splitAccessor = {
    get value() { return 1; },
    set value(next) { throw "setter"; },
};
function readsSafeGetter() {
    return splitAccessor.value;
}
function writesThrowingSetter() {
    splitAccessor.value = 2;
}
// Structural assignment cannot erase a getter or setter effect by flowing an
// accessor into a concrete data-property contract.
class DataBox {
    value = 1;
}
const getterSource = { get value() { throw "erased getter"; } };
const getterErasure = getterSource;
const setterSource = {
    get value() { return 1; },
    set value(next) { throw "erased setter"; },
};
const setterErasure = setterSource;
const assertedGetterErasure = getterSource;
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
