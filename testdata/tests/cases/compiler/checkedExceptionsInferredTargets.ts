// @checkedExceptions: true
// @strict: true

class E1 extends Error { readonly tag = "e1" as const; }
class E2 extends Error { readonly tag = "e2" as const; }

declare const throwsE2: () => void throws E2;

// A mutable binding whose type carries *inferred* throws constrains
// reassignment: call sites of `f` see the initializer's inferred set (E1),
// so assigning a wider thrower must fail.
let f = (x?: number) => { if (x) throw new E1(); };
f = throwsE2; // error
f = (x?: number) => { if (x) throw new E1(); }; // ok: same set

// Same through a function declaration's type.
function named(x?: number) { if (x) throw new E1(); }
let h = named;
h = throwsE2; // error

// An initializer whose visible body throws nothing is proven safe, so a
// throwing function cannot overwrite the binding.
let g = (x?: number) => {};
g = throwsE2; // error

// Catch variables demanded while inference is in flight are not pinned to
// unknown; the precise union is computed once inference completes.
function outer(): void {
    try {
        f();
    } catch (e) {
        const t: "e1" = e.tag;
    }
}
outer();
