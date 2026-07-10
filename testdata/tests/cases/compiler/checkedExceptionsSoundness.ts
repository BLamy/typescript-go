// @checkedExceptions: true
// @strict: true

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

// Retained accessors, constructors, and factories are executable capabilities
// too. A catch around the registration call cannot observe their later throws.
class LateGetter {
    get value(): number { throw "late getter"; }
}
declare function retainValue(value: LateGetter): void throws never;
retainValue(new LateGetter());

class LateConstructor {
    constructor() { throw "late constructor"; }
}
declare function retainConstructor(ctor: typeof LateConstructor): void throws never;
retainConstructor(LateConstructor);

function lateFactory() {
    return () => { throw "late returned callback"; };
}
declare function retainFactory(factory: typeof lateFactory): void throws never;
retainFactory(lateFactory);

// Explicit member contracts are checked even if the member is never invoked.
class MemberContracts {
    constructor() throws never { throw "constructor contract"; }
    method(): void throws never { throw "method contract"; }
    get value(): number throws never { throw "getter contract"; }
    set value(next: number) throws never { throw "setter contract"; }
}

class LyingConstructorOverload {
    constructor() throws never;
    constructor() { throw "constructor implementation"; }
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
const getterOnlySource = { get value(): number { return 1; } };
const getterOnlyWriteErasure: DataBox = getterOnlySource;
const assertedGetterOnlyWriteErasure = getterOnlySource as DataBox;

abstract class AbstractSafeData {
    abstract value: number;
}
class ThrowingAccessorOverride extends AbstractSafeData {
    get value(): number { throw "override accessor"; }
}
class SafeMethodBase {
    method(): void throws never {}
}
class ThrowingMethodOverride extends SafeMethodBase {
    method(): void { throw "override method"; }
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

// Function types without a clause describe an unknown effect when enabled.
type LegacyCallback = () => void;
type SafeCallback = () => void throws never;
declare const legacyCallback: LegacyCallback;
const incorrectlyNarrowed: SafeCallback = legacyCallback;

// `any` cannot manufacture a checked-effect proof through ordinary
// assignment; doing so would bypass the assertion guard and erase effects.
declare const dynamicCapability: any;
const anyToSafeCallback: SafeCallback = dynamicCapability;
const anyToSafeData: DataBox = dynamicCapability;
type LegacyOptions = { callback: LegacyCallback };
type SafeOptions = { callback: SafeCallback };
declare const legacyOptions: LegacyOptions;
const nestedAssertionErasesEffect = legacyOptions as SafeOptions;
const anyNestedAssertionErasesEffect = dynamicCapability as SafeOptions;
type LegacyConstructorType = new () => DataBox;
type SafeConstructorType = new () => DataBox throws never;
declare const legacyConstructorValue: LegacyConstructorType;
const constructorAssertionErasesEffect = legacyConstructorValue as SafeConstructorType;
function genericAnyLaunder<T>(value: any): T {
    return value;
}
function genericAssertionLaunder<T>(value: any): T {
    return value as T;
}
function mutationThrower(): void throws "mutated alias" { throw "mutated alias"; }
const safeMutationVictim: { callback: SafeCallback } = { callback: () => {} };
function anyAliasCannotCorruptSafeCallback(): void throws never {
    try {
        (safeMutationVictim as any).callback = mutationThrower;
        delete (safeMutationVictim as any).callback;
    } catch {
    }
    safeMutationVictim.callback();
}
function genericRaise<T>(): void throws T { throw null as T; }
try {
    throw dynamicCapability;
} catch (e) {
    const rawAnyMustBeUnknown: string = e;
}
try {
    genericRaise<any>();
} catch (e) {
    const instantiatedAnyMustBeUnknown: string = e;
}

// Proxy preserves T in today's lib declaration but can add latent traps,
// revocation, and private-brand failures that T cannot represent.
class PrivateBox {
    #value = 1;
    static read(box: PrivateBox): number throws never { return box.#value; }
}
function proxyCannotMasqueradeAsPrivateBox(): PrivateBox throws never {
    try {
        return new Proxy(new PrivateBox(), {});
    } catch {
        return new PrivateBox();
    }
}

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
