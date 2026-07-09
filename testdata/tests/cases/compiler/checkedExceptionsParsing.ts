// @checkedExceptions: error
// @strict: true
// @declaration: true

class E1 extends Error { readonly tag = "e1" as const; }
class E2 extends Error { readonly tag = "e2" as const; }

// Clause forms on every supported declaration shape.
function decl(): void throws E1 {}
const funcExpr = function (): void throws E1 {};
const arrow = (): void throws E1 => {};
class C {
    method(): void throws E1 {}
}
interface I {
    method(): void throws E1 | E2;
}
type FnType = (x: string) => number throws E1;
interface Callable {
    (x: string): number throws E1;
}
type CallableLiteral = {
    (x: string): number throws E1 | E2;
};
const obj = {
    method(): void throws E1 {},
};

// A clause without a return type annotation.
function noReturnType() throws E1 {
    throw new E1();
}

// The clause type may be any type expression, greedily parsed.
function unionClause(): void throws E1 | E2 {}
type Alias = E1 | E2;
function aliasClause(): void throws Alias {}
function genericClause<T extends Error>(x: T): void throws T {
    throw x;
}

// `throws` is contextual: members named `throws` on a following line still work.
interface Legacy {
    m(): void
    throws: boolean
}
declare const legacy: Legacy;
const b: boolean = legacy.throws;

class LegacyClass {
    m(): void {}
    throws = 1;
}

const throwsAsName = { throws: true };
function throws(): void {}
throws();
