//// [tests/cases/compiler/checkedExceptionsRecursionAndEdgeCases.ts] ////

//// [checkedExceptionsRecursionAndEdgeCases.ts]
class E1 extends Error { readonly tag = "e1" as const; }
class E2 extends Error { readonly tag = "e2" as const; }

// Finding 1: symmetric mutual recursion must report symmetrically.
function f(x: number): void { if (x > 0) throw new E1(); g(x - 1); }
function g(x: number): void { if (x > 0) throw new E2(); f(x - 1); }
function wrapF(): void throws E1 { f(3); }  // error: E2 missing
function wrapG(): void throws E2 { g(3); }  // error: E1 missing

// Finding 2: mutually recursive rethrow wrappers, no TS7022, still enforced.
function a(): void { try { b(); } catch (e) { throw e; } }
function b(): void { try { a(); } catch (e) { throw e; } throw new E1(); }
a(); // error: E1 surfaces at top level

// Finding 3: union with an untracked constituent keeps the tracked side.
declare const uMixed: (() => void throws E1) | (() => void);
uMixed(); // error: E1

// Finding 4: default parameter initializers participate in inference.
declare function boom(): number throws E1;
function noClause(x = boom()) { return x; }
noClause(); // error: E1

// Parser F1: parenless-return-type arrows with clauses.
const t1 = () throws E1 => { throw new E1(); };
const t2 = async () throws E1 => { throw new E1(); };
function takesT1(): void throws E1 { t1(); }

// Checker F3: unresolved clause type reports even when never called.
declare function neverCalled(): void throws CompletelyUndefinedName;


//// [checkedExceptionsRecursionAndEdgeCases.js]
"use strict";
class E1 extends Error {
    tag = "e1";
}
class E2 extends Error {
    tag = "e2";
}
// Finding 1: symmetric mutual recursion must report symmetrically.
function f(x) { if (x > 0)
    throw new E1(); g(x - 1); }
function g(x) { if (x > 0)
    throw new E2(); f(x - 1); }
function wrapF() { f(3); } // error: E2 missing
function wrapG() { g(3); } // error: E1 missing
// Finding 2: mutually recursive rethrow wrappers, no TS7022, still enforced.
function a() { try {
    b();
}
catch (e) {
    throw e;
} }
function b() { try {
    a();
}
catch (e) {
    throw e;
} throw new E1(); }
a(); // error: E1 surfaces at top level
uMixed(); // error: E1
function noClause(x = boom()) { return x; }
noClause(); // error: E1
// Parser F1: parenless-return-type arrows with clauses.
const t1 = () => { throw new E1(); };
const t2 = async () => { throw new E1(); };
function takesT1() { t1(); }
