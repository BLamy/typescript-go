//// [tests/cases/compiler/checkedExceptionsAssignability.ts] ////

//// [checkedExceptionsAssignability.ts]
class IOError extends Error { readonly tag = "io" as const; }
class ParseError extends Error { readonly tag = "parse" as const; }

declare function risky(): void throws IOError;

type SafeFn = () => void throws never;
type IOFn = () => void throws IOError;
type WideFn = () => void throws IOError | ParseError;
type UntrackedFn = () => void;

declare const ioFn: IOFn;

// Widening is fine (covariance).
const widened: WideFn = ioFn;

// Narrowing is an error.
const narrowed: SafeFn = ioFn;

// A function that throws nothing is assignable anywhere.
declare const safeFn: SafeFn;
const safeAsIO: IOFn = safeFn;
const safeAsUntracked: UntrackedFn = safeFn;

// Contextual arrows: inferred throws are checked against the target clause.
const okHandler: SafeFn = () => {};
const badHandler: SafeFn = () => { risky(); }; // error
const okDeclared: IOFn = () => { risky(); };

// Untracked declarations (no clause, no body) are treated permissively.
declare function legacy(): void;
const legacyAsSafe: SafeFn = legacy;

// `throws unknown` targets accept anything.
type AnythingFn = () => void throws unknown;
const anything: AnythingFn = ioFn;


//// [checkedExceptionsAssignability.js]
"use strict";
class IOError extends Error {
    tag = "io";
}
class ParseError extends Error {
    tag = "parse";
}
// Widening is fine (covariance).
const widened = ioFn;
// Narrowing is an error.
const narrowed = ioFn;
const safeAsIO = safeFn;
const safeAsUntracked = safeFn;
// Contextual arrows: inferred throws are checked against the target clause.
const okHandler = () => { };
const badHandler = () => { risky(); }; // error
const okDeclared = () => { risky(); };
const legacyAsSafe = legacy;
const anything = ioFn;
