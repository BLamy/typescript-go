//// [tests/cases/compiler/checkedExceptionsOverloads.ts] ////

//// [checkedExceptionsOverloads.ts]
class E1 extends Error { readonly tag = "e1" as const; }
class E2 extends Error { readonly tag = "e2" as const; }

// Overloads may declare different throws types; the implementation's wider
// clause is compatible (throws rides the return channel, which
// overload/implementation compatibility ignores).
function pick(x: string): string throws E1;
function pick(x: number): number throws E2;
function pick(x: string | number): string | number throws E1 | E2 {
    if (typeof x === "string") throw new E1();
    throw new E2();
}

// Call sites use the resolved overload's clause.
function callsString(): void throws E1 {
    pick("a");
}
function callsNumber(): void throws E1 {
    pick(1); // error: resolves to the E2 overload
}
function catchesOverload() {
    try {
        pick("x");
    } catch (e) {
        const t: "e1" = e.tag; // e: E1, from the matched overload
        return t;
    }
}

// A union-typed callee raises the union of all constituents' clauses.
declare const fn: ((x: string) => void throws E1) | ((x: string) => void throws E2);
function callsUnion(): void throws E1 | E2 {
    fn("a");
}
function callsUnionWrong(): void throws E1 {
    fn("a"); // error: E2 is not declared
}


//// [checkedExceptionsOverloads.js]
"use strict";
class E1 extends Error {
    tag = "e1";
}
class E2 extends Error {
    tag = "e2";
}
function pick(x) {
    if (typeof x === "string")
        throw new E1();
    throw new E2();
}
// Call sites use the resolved overload's clause.
function callsString() {
    pick("a");
}
function callsNumber() {
    pick(1); // error: resolves to the E2 overload
}
function catchesOverload() {
    try {
        pick("x");
    }
    catch (e) {
        const t = e.tag; // e: E1, from the matched overload
        return t;
    }
}
function callsUnion() {
    fn("a");
}
function callsUnionWrong() {
    fn("a"); // error: E2 is not declared
}
