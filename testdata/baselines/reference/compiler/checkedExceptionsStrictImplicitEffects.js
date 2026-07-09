//// [tests/cases/compiler/checkedExceptionsStrictImplicitEffects.ts] ////

//// [checkedExceptionsStrictImplicitEffects.ts]
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


//// [checkedExceptionsStrictImplicitEffects.js]
"use strict";
var __addDisposableResource = (this && this.__addDisposableResource) || function (env, value, async) {
    if (value !== null && value !== void 0) {
        if (typeof value !== "object" && typeof value !== "function") throw new TypeError("Object expected.");
        var dispose, inner;
        if (async) {
            if (!Symbol.asyncDispose) throw new TypeError("Symbol.asyncDispose is not defined.");
            dispose = value[Symbol.asyncDispose];
        }
        if (dispose === void 0) {
            if (!Symbol.dispose) throw new TypeError("Symbol.dispose is not defined.");
            dispose = value[Symbol.dispose];
            if (async) inner = dispose;
        }
        if (typeof dispose !== "function") throw new TypeError("Object not disposable.");
        if (inner) dispose = function() { try { inner.call(this); } catch (e) { return Promise.reject(e); } };
        env.stack.push({ value: value, dispose: dispose, async: async });
    }
    else if (async) {
        env.stack.push({ async: true });
    }
    return value;
};
var __disposeResources = (this && this.__disposeResources) || (function (SuppressedError) {
    return function (env) {
        function fail(e) {
            env.error = env.hasError ? new SuppressedError(e, env.error, "An error was suppressed during disposal.") : e;
            env.hasError = true;
        }
        var r, s = 0;
        function next() {
            while (r = env.stack.pop()) {
                try {
                    if (!r.async && s === 1) return s = 0, env.stack.push(r), Promise.resolve().then(next);
                    if (r.dispose) {
                        var result = r.dispose.call(r.value);
                        if (r.async) return s |= 2, Promise.resolve(result).then(next, function(e) { fail(e); return next(); });
                    }
                    else s |= 1;
                }
                catch (e) {
                    fail(e);
                }
            }
            if (s === 1) return env.hasError ? Promise.reject(env.error) : Promise.resolve();
            if (env.hasError) throw env.error;
        }
        return next();
    };
})(typeof SuppressedError === "function" ? SuppressedError : function (error, suppressed, message) {
    var e = new Error(message);
    return e.name = "SuppressedError", e.error = error, e.suppressed = suppressed, e;
});
// A precisely declared direct call remains usable in strict code.
function directSafeCall() {
    safe();
}
// Existing standard-library declarations have no throws metadata, so strict
// mode treats them as unknown rather than claiming they are safe.
function parseJson() {
    return JSON.parse("{");
}
function methodLookup() {
    service.run();
}
function elementLookup() {
    keyedService["run"]();
}
function constructLegacy() {
    return new LegacyConstructor();
}
function callUnion() {
    unionFunction();
}
function callLegacyOverload() {
    overloaded(1);
}
function defineClass() {
    class Local {
        static value = legacy();
    }
}
// Catching an unknown standard-library boundary is sound, but the binding must
// remain unknown.
function catchJson() {
    try {
        JSON.parse("{");
    }
    catch (e) {
        const value = e;
    }
}
function coercion() {
    return dynamicValue + 1;
}
function unaryCoercion() {
    return +dynamicValue;
}
function safePredicates(value) {
    return !(value === null);
}
function spreadIterable() {
    return [...iterable];
}
function iterate() {
    for (const value of iterable) {
        safe();
    }
}
function taggedTemplate() {
    return tag `value`;
}
function destructuredParameter({ value }) {
    safe();
}
// Explicit resource management invokes a disposal hook at scope exit.
function disposalAtScopeExit() {
    const env_1 = { stack: [], error: void 0, hasError: false };
    try {
        const resource = __addDisposableResource(env_1, null, false);
    }
    catch (e_1) {
        env_1.error = e_1;
        env_1.hasError = true;
    }
    finally {
        __disposeResources(env_1);
    }
}
let assignedValue;
function destructuringAssignment() {
    ({ value: assignedValue } = destructuringSource);
}
