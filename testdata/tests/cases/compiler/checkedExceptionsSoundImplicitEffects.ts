// @checkedExceptions: true
// @strict: true

// Minimal global required by the explicit-resource-management grammar in this
// focused baseline; the value below is null, so no member shape is needed.
interface Disposable {}

declare function safe(): void throws never;
declare function known(): void throws "known";

// A precisely declared direct call remains usable in strict code.
function directSafeCall(): void throws never {
    safe();
}

// Existing standard-library declarations have no throws metadata, so strict
// mode treats them as unknown rather than claiming they are safe.
function parseJson(): unknown throws never {
    return JSON.parse("{");
}

// Fetching a method may itself invoke a getter or Proxy trap, even when the
// resolved method signature explicitly promises `throws never`.
declare const service: {
    run(): void throws never;
};
function methodLookup(): void throws never {
    service.run();
}

declare const keyedService: {
    run(): void throws never;
};
function elementLookup(): void throws never {
    keyedService["run"]();
}

// Construct signatures without throws metadata are unknown boundaries.
declare const LegacyConstructor: new () => object;
function constructLegacy(): object throws never {
    return new LegacyConstructor();
}

// An untracked constituent poisons a union call instead of disappearing.
type KnownFunction = () => void throws "known";
type LegacyFunction = () => void;
declare const unionFunction: KnownFunction | LegacyFunction;
function callUnion(): void throws "known" {
    unionFunction();
}

// The selected legacy overload is unknown.
declare function overloaded(value: 0): void throws "known";
declare function overloaded(value: number): void;
function callLegacyOverload(): void throws never {
    overloaded(1);
}

// Class evaluation can execute static initializers, computed names, and
// decorators. It remains unknown until those effects are modeled separately.
declare function legacy(): void;
function defineClass(): void throws never {
    class Local {
        static value = legacy();
    }
}

// Catching an unknown standard-library boundary is sound, but the binding must
// remain unknown.
function catchJson(): void throws never {
    try {
        JSON.parse("{");
    } catch (e) {
        const value: unknown = e;
    }
}

// Operators that can perform user coercion fail closed, while strict equality
// and logical negation remain proven-safe operations.
declare const dynamicValue: any;
function coercion(): unknown throws never {
    return dynamicValue + 1;
}
function unaryCoercion(): number throws never {
    return +dynamicValue;
}
function safePredicates(value: unknown): boolean throws never {
    return !(value === null);
}

// Iteration, spreading, tagged templates, and parameter destructuring all
// execute protocol hooks or property reads that can throw.
declare const iterable: Iterable<number>;
function spreadIterable(): number[] throws never {
    return [...iterable];
}
function iterate(): void throws never {
    for (const value of iterable) {
        safe();
    }
}

declare const tag: (parts: TemplateStringsArray) => string throws never;
function taggedTemplate(): string throws never {
    return tag`value`;
}

function destructuredParameter({ value }: { value: number }): void throws never {
    safe();
}

// Explicit resource management invokes a disposal hook at scope exit.
function disposalAtScopeExit(): void throws never {
    using resource = null;
}

declare const destructuringSource: { value: number };
let assignedValue: number;
function destructuringAssignment(): void throws never {
    ({ value: assignedValue } = destructuringSource);
}
