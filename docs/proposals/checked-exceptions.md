# Proposal: Checked Exceptions & First-Class Error Tracking for TypeScript

- **Status:** Draft / Request for Comments
- **Champion:** _(unassigned)_
- **Target:** TypeScript language + `tsc` (including the native Go port, `typescript-go`)
- **Feature flag:** `checkedExceptions` (`boolean`, default `false`)

---

## 1. Summary

TypeScript models almost everything about a program's data flow in the type
system — except the one control-flow path that most often breaks in production:
the one where something goes wrong. A function's return type is checked, but the
set of errors it can `throw` is invisible. `catch` clauses hand you `unknown`
(or `any`), and there is no compiler-level guarantee that a caller has accounted
for a failure a callee can produce.

This proposal adds **typed, tracked errors** to TypeScript. A function may
declare the error types it can raise with a Java-inspired `throws` clause, and
the checker enforces that every such error is either **handled** (caught and
narrowed) or **propagated** (re-declared on the caller). The enforcement is
gated behind a single boolean compiler option, `checkedExceptions`. When it is
enabled, the checker uses one fail-closed contract; when disabled, the syntax
has no checking effect.

The design goal is to bring the *safety* of [Effect.ts](https://effect.website/)'s
typed error channel to *idiomatic* TypeScript — using ordinary `throw` / `try` /
`catch`, not a wrapper monad — while keeping the entire feature explicitly
opt-in.

## 2. Motivation

### 2.1 The problem today

```ts
function readConfig(path: string): Config {
  const raw = fs.readFileSync(path, "utf8"); // can throw: ENOENT, EACCES...
  return JSON.parse(raw);                     // can throw: SyntaxError
}

// Caller has no idea this can fail, and the compiler will never tell them.
const config = readConfig("./app.json");
```

Three well-known gaps:

1. **`throw` is untyped.** You can `throw` any value. The signature of
   `readConfig` says `(path: string) => Config`, which is a lie by omission: it
   is really "returns a `Config`, *or* aborts with a `SyntaxError` or a Node
   `ErrnoException`."
2. **`catch` is untyped.** Even with `useUnknownInCatchVariables`, the best the
   compiler gives you is `unknown`. You must hand-write runtime type guards and
   hope you covered every case.
3. **No propagation checking.** Nothing forces a caller to acknowledge a
   callee's failure modes, so errors silently bubble to the top of the stack
   and become uncaught-exception crashes or unhandled promise rejections.

### 2.2 Why not just use Effect / neverthrow / Result types?

Libraries like Effect.ts, `neverthrow`, and `fp-ts` solve this by moving errors
into the *value* channel — `Effect<A, E, R>`, `Result<T, E>`. This works and is
excellent, but it has costs that keep it out of most codebases:

- It requires rewriting call sites into a monadic style (`pipe`, `flatMap`,
  generators, `yield*`) — an all-or-nothing ergonomic commitment.
- It doesn't describe the millions of existing functions (and the entire DOM /
  Node / npm ecosystem) that communicate failure by `throw`ing.
- Interop with `throw`-based code (including every `JSON.parse`, `await`, and
  third-party callback) requires manual bridging.

**This proposal takes the complementary approach: track errors on the *effect*
channel that TypeScript programs already use — `throw`.** You keep writing
normal code; the type system starts describing the failures that were always
there.

### 2.3 Prior art

- **Java** — checked exceptions via `throws`. The direct syntactic inspiration.
  We learn from its ergonomic failures (see §9).
- **Effect.ts** — typed error channel, the semantic target for expressiveness.
- **Swift** — `throws` / `try` / `rethrows`, and typed throws (`throws(E)`) as
  of Swift 6. The closest modern analogue to what we propose.
- **Rust** — `Result<T, E>` + `?`. Errors as values; different channel, same goal.
- **CheckedTypeScript** and various community `@throws` JSDoc linters —
  demonstrate demand and partial precedent.

## 3. Guiding principles

1. **Opt-in, always.** With `checkedExceptions: false` (the default), the new
   syntax parses and is available for tooling, but produces **zero** new
   diagnostics. Existing code and existing `tsconfig` files compile unchanged.
2. **One sound contract.** Enabling the feature never silently weakens an
   unknown effect for migration. Adoption happens at a project boundary by
   annotating declarations and explicitly catching or propagating unknowns.
3. **Idiomatic.** Uses `throw` / `try` / `catch`. No new runtime, no monad, no
   boxing. **Zero emit impact** — throws clauses are erased like type
   annotations.
4. **Structural, inferred, and honest at boundaries.** Consistent with
   TypeScript, error sets are inferred where possible and behave structurally.
   Enabled analysis represents missing information as `unknown`; it never treats an
   unannotated external boundary as proof that no exception is possible.
5. **Fail closed.** Precision may improve over time, but an unclassified effect
   is always `unknown`, never silently omitted.

### 3.1 What “sound” means here

The claim is an **effect-soundness** claim, not a claim that TypeScript
becomes a fully sound runtime type system. For a program accepted without
diagnostic suppression, every modeled catchable abrupt completion caused by
executing checked TypeScript is either:

1. discharged by a control-flow boundary that can actually observe it,
2. included in the function or promise rejection effect exposed to its caller,
   or
3. rejected at an escaping boundary, such as a potentially retained callback.

As with all foreign-function interfaces, explicit declaration clauses are
trusted contracts. An ambient function declared `throws never` while its native
implementation throws is a lying declaration, just as an ambient function
declared to return `string` while returning a number lies to today's checker.
Missing ambient metadata is never trusted when checked exceptions are enabled: it becomes
`throws unknown`. Diagnostic suppression, unchecked generated JavaScript,
runtime code mutation, and resource-exhaustion/engine termination are outside
the claim. Callable type assertions are not allowed to narrow an effect.

This boundary is essential: a useful TypeScript feature can soundly track
effects relative to checked source and trusted declarations, but it cannot
prove arbitrary native code, monkey-patched runtime state, or ignored
diagnostics. The testable invariant is therefore **no silent effect loss across
an untrusted or dynamically executable boundary**.

## 4. Syntax

### 4.1 The `throws` clause

A `throws` clause may appear on any function-like declaration, immediately
after the return-type annotation:

```ts
function readFile(path: string): string throws IOError { ... }

const parse = (s: string): Json throws SyntaxError => JSON.parse(s);

class Store {
  get(key: string): Value throws NotFoundError | IOError { ... }
}

interface Api {
  fetchUser(id: string): Promise<User> throws NetworkError;
}
```

The clause takes an arbitrary **type** — the *error type* — which is treated as
a set via union:

```ts
function f(): void throws NetworkError | TimeoutError { ... }
type FsError = ENOENT | EACCES;
function g(): Data throws FsError { ... }        // aliases allowed
function h(): void throws never { ... }          // explicitly throws nothing
```

`throws never` is the explicit "this function does not throw" declaration
(analogous to Swift's non-`throws`). The absence of a clause is *not* the same
as `throws never` — see §6.4.

### 4.2 Typed `catch`

When `checkedExceptions` is enabled, the type of a `catch` binding is **narrowed
to the union of error types that the `try` block can actually raise**, rather
than always being `unknown`:

```ts
try {
  const s = readFile("a.txt"); // throws IOError
  return parse(s);             // throws SyntaxError
} catch (e) {
  // e: IOError | SyntaxError   (instead of unknown)
  if (e instanceof SyntaxError) { ... }
  else { ... } // e: IOError
}
```

This is the single most valuable ergonomic win of the feature and is available
even for code that does not itself declare `throws` clauses, as long as the
callees do.

### 4.3 Handling vs. propagating

Inside a function body, every error a statement can raise must be discharged in
one of two ways:

- **Handled** — wrapped in a `try` whose `catch` is *exhaustive* for that error
  type (or which re-throws only declared types), or
- **Propagated** — declared in the enclosing function's own `throws` clause.

```ts
// Propagate: readFile's IOError is re-declared here.
function load(path: string): Config throws IOError | SyntaxError {
  const raw = readFile(path); // IOError propagated ✔
  return parse(raw);          // SyntaxError propagated ✔
}

// Handle: nothing escapes, so no throws clause is needed.
function loadOrDefault(path: string): Config {
  try {
    return load(path);
  } catch (e) {
    // e: IOError | SyntaxError — fully handled ✔
    return DEFAULT_CONFIG;
  }
}
```

If neither is done, the checker reports the error at the offending call site
(§7).

### 4.4 `rethrows` for higher-order functions

A function that only throws *because its callback throws* can be marked
`rethrows`. Its effective throws set is that of the function-typed arguments it
is given — this is what makes `Array.prototype.map` usable without forcing every
caller through a `throws any` funnel:

```ts
interface Array<T> {
  map<U>(fn: (x: T) => U): U[] rethrows;
}

// map itself declares no fixed error type; it inherits fn's.
const nums = ["1", "2"].map(s => parseStrict(s)); // throws whatever parseStrict throws
```

`rethrows` is the mechanism that keeps the standard library and combinators
(`forEach`, `filter`, `then`, event handlers, etc.) from becoming adoption
blockers. See §8.2.

### 4.5 Runtime-filtered `catch match`

The TC39 pattern-matching proposal sketches conditional catch clauses whose
unmatched exceptions are rethrown automatically:

```ts
try {
  loadConfig();
} catch match (e) {
  when IOError: recoverIO(e);
  when { code: "E_PARSE" }: recoverParse(e);
  // Anything not matched continues outward.
}
```

This is stronger than a type annotation on `catch`: each arm performs a runtime
test before TypeScript narrows the binding. If the try block has effect `E` and
the patterns are proven to cover `H`, the catch body discharges `H` and the
residual effect `Exclude<E, H>` is rethrown. An exhaustive wildcard/default arm
discharges the whole set. When `E` is `unknown`, finite patterns cannot make the
catch exhaustive; only an explicit wildcard/default arm can discharge the
unknown remainder.

Pattern evaluation is itself effectful. Structural patterns may invoke getters,
iterators, Proxy traps, or custom matchers; the TC39 proposal specifies that an
exception thrown by a custom matcher propagates. The checker therefore adds the
pattern's own effect to the surrounding function rather than pretending the
filter is pure. This integration remains gated on the TC39 syntax, which is
currently a possible future enhancement rather than settled ECMAScript syntax.

## 5. Type-system semantics

### 5.1 Error sets are part of the call signature

Every call signature gains an optional **throws type** `E` (default: the
inference rules in §6). A signature is written internally as
`(params) => Return throws E`. Two signatures relate as follows.

### 5.2 Assignability / subtyping

A function `f` is assignable to a function type `g` only if, in addition to the
usual parameter/return rules, **`f`'s throws set is assignable to `g`'s throws
set**:

```
(a: A) => R throws E1   assignable to   (a: A) => R throws E2
    requires   E1 ⊆ E2   (E1 assignable to E2)
```

That is, throws behaves **covariantly** like the return type: a function that
throws *fewer/narrower* errors is substitutable where *more/wider* errors are
allowed. `throws never` is the bottom of this lattice (assignable everywhere);
`throws unknown` / `throws any` is the top.

```ts
declare let handler: (x: string) => void throws NetworkError;
const a: (x: string) => void throws NetworkError | TimeoutError = handler; // ✔ widen
const b: (x: string) => void throws never = handler;                       // ✘ NetworkError not ⊆ never
```

This makes `throws never` functions freely usable where a throwing function is
expected, and prevents a wide thrower from masquerading as a narrow one.

### 5.3 The error set is a structural union

Error types compose with normal union/narrowing machinery. There is nothing new
to learn about *how* the types combine — `throws A | B` is exactly the union
`A | B`, and `catch` narrowing reuses the existing control-flow narrowing
(`instanceof`, discriminated unions, user-defined type guards).

We recommend (but do not require) that error types be **discriminated** — e.g. a
`readonly _tag: "NetworkError"` or `name` field — so `catch` blocks can exhaust
them with a `switch`, exactly as one discriminates any other union. This is the
same pattern Effect uses (`Data.TaggedError`).

### 5.4 Async and promises

`async` functions raise errors as *promise rejections*, but from the source's
point of view they are written with `throw` and observed with `try/catch` around
`await`. The throws clause of an `async` function describes the **rejection
type**:

```ts
async function fetchUser(id: string): Promise<User> throws NetworkError {
  const res = await fetch(id);      // fetch: throws NetworkError
  if (!res.ok) throw new NetworkError(res.status);
  return res.json();
}

async function main() {
  try {
    const u = await fetchUser("1"); // await surfaces the rejection
  } catch (e) {
    // e: NetworkError
  }
}
```

Rules:

- The clause is written on the function, describing what a consumer sees when
  they `await` the returned promise (i.e. the rejection value), **not**
  `Promise<...>` wrapping the error.
- `await p` on an expression of type `Promise<T>` carrying tracked error `E`
  contributes `E` to the surrounding try/function, exactly as a synchronous call
  would.
- A floating promise (not `await`ed, not `.catch`ed) with a non-`never` tracked
  rejection is reported under the same option — this subsumes and strengthens
  the existing `no-floating-promises` lint at the type level.
- A synchronous `try/catch` around a promise-producing call does not discharge
  its rejection effect. In the reference implementation, a call whose
  resolved return type is promise-like must currently be immediately `await`ed
  or directly returned. This deliberately rejects more complex promise chains
  until their rejection transformations can be represented precisely.
- Directly returning a promise propagates the callee's rejection effect through
  the enclosing function's clause. An immediate `await` transfers that effect
  into the surrounding synchronous control-flow position, where an enclosing
  `catch` can discharge it.
- Callback effects require an invocation-timing contract. A callback retained
  for an event or timer may throw after both the caller's `try/catch` and its
  `throws` boundary have disappeared. Until `rethrows` distinguishes proven
  synchronous invocation from an escaping callback, checked exceptions require a
  function-valued argument with a non-`never` effect to handle that effect
  inside the callback itself. A surrounding synchronous catch is not proof.
- The same lifetime rule applies to every executable capability passed as an
  argument, not only a directly callable value. Throwing getters, constructors,
  methods nested in option objects, and factories that return throwing
  callbacks can also be retained and executed after the call returns. Until a
  parameter contract proves non-retention, their effects must be discharged
  inside the capability.

To represent a promise's rejection type in the structural type system, we extend
the `Promise<T>` relationship with an optional second tracked slot; see §10.2
for the `lib.d.ts` strategy.

## 6. Inference — keeping it TypeScript, not Java

Java's checked exceptions are widely disliked primarily because they are
**mandatory and un-inferred**: every `throws` must be written by hand and
threaded manually up the call stack. TypeScript's whole personality is
inference, so the throws set is inferred aggressively.

### 6.1 Inferred throws for un-annotated functions

For a function **without** an explicit `throws` clause, the checker *infers* its
throws set from its body: the union of

- the types of all reachable `throw` expressions, and
- the (inferred or declared) throws sets of all calls not enclosed in a
  handling `try`,

minus anything fully handled by an enclosing `catch`. This is the same
fixpoint/flow analysis already used for control-flow and return-type inference,
extended to the throws channel.

```ts
// No clause written; inferred as `throws SyntaxError | IOError`.
function load(path: string) {
  return parse(readFile(path));
}
```

This means **local, un-annotated code keeps working** and still gets accurate
`catch` narrowing, without the Java tax of writing clauses everywhere.

### 6.2 Where annotations are required

Explicit `throws` clauses are only *required* at **inference boundaries**, where
TypeScript already requires return-type discipline:

- exported/public API surface when `--declaration` is on (a `.d.ts` must state
  the throws set, just as it states the return type);
- overload signatures and ambient declarations;
- recursive functions whose throws set cannot be inferred without a fixpoint
  annotation.

Everywhere else, inference does the work. This is the key ergonomic departure
from Java.

### 6.3 Contextual typing flows throws inward

When a function expression is contextually typed by a target that declares a
throws set, that set flows in — so callbacks passed to a `rethrows` API, or
assigned to a typed variable, are checked against the expected error type
without repeating it:

```ts
type Handler = (e: Event) => void throws never;
const h: Handler = (e) => { risky(); }; // ✘ error: risky() throws, but Handler is `throws never`
```

### 6.4 The meaning of "no clause"

An absent clause must never be interpreted as proof of `throws never`:

| Context | Interpretation of a missing `throws` clause |
|---|---|
| Local function with a visible body | **Inferred** from the body (§6.1). |
| `checkedExceptions: false` | Ignored. |
| Ambient / `.d.ts` / no visible body, `checkedExceptions: true` | Always `throws unknown`; callers must catch it or propagate `unknown`. |

A future `@throws` JSDoc tag and a `lib` upgrade (§10.2) let ambient
declarations opt into precise sets over time.

## 7. Diagnostics & the `checkedExceptions` option

### 7.1 The option

```jsonc
// tsconfig.json
{
  "compilerOptions": {
    "checkedExceptions": true
  }
}
```

- **`false`** — default. Syntax parses and `throws` clauses remain available to
  declaration emit and tooling, but catch variables and diagnostics retain
  today's behavior. Fully backward compatible.
- **`true`** — fail closed and report build-blocking errors. Missing declaration information, explicit
  `throws any`, structural property effects that cannot yet be proven safe, and
  other unclassified boundaries contribute `unknown`. Unknown effects must be
  caught or propagated and poison optimistic catch narrowing.

A matching CLI flag (`--checkedExceptions`) enables the same contract. The
option is not implied by TypeScript's existing `strict` flag, so upgrades do
not silently enable a new effect system.

### 7.2 Primary diagnostics

- **TSxxx1: "Call to '{0}' may throw '{1}', which is not handled or declared."**
  — emitted at the call site when a tracked error is neither caught by an
  enclosing handling `try` nor a member of the enclosing function's `throws`
  set.
- **TSxxx2: "Throws clause of '{0}' declares '{1}', which is never raised."** —
  an unused-declaration hint (suggestion-level by default) analogous to
  `noUnusedLocals`; helps clauses stay honest.
- **TSxxx3: "Catch clause is not exhaustive for '{0}'; '{1}' may still
  propagate."** — when a `catch` re-throws or a `try` only partially handles.

### 7.3 Suppression and imprecision

- `throws unknown` (or `throws any`) on a function explicitly opts it out of
  *precise* tracking. The checker normalizes both to the top `unknown` effect, which callers must
  catch or propagate; giving up on precision never means claiming safety.
- A `try { ... } catch (e: unknown) { /* swallow */ }` with an intentionally
  wide binding fully discharges everything, matching current semantics.
- Line-level `// @ts-expect-error` / directive suppression works as with any
  diagnostic.

## 8. Worked examples

### 8.1 The motivating example, tracked

```ts
class IOError extends Error { readonly _tag = "IOError" as const; }
class ParseError extends Error { readonly _tag = "ParseError" as const; }

function readFile(path: string): string throws IOError {
  if (!exists(path)) throw new IOError(path);
  return raw(path);
}

function parseConfig(text: string): Config throws ParseError {
  try { return JSON.parse(text); }
  catch { throw new ParseError(text); } // normalize untyped throw into a tracked one
}

// Inferred: throws IOError | ParseError
function readConfig(path: string) {
  return parseConfig(readFile(path));
}

function main() {
  try {
    const cfg = readConfig("./app.json");
    run(cfg);
  } catch (e) {
    // e: IOError | ParseError
    switch (e._tag) {
      case "IOError":    console.error("cannot read:", e.message); break;
      case "ParseError": console.error("bad config:", e.message); break;
    } // exhaustive ✔ — nothing escapes main
  }
}
```

Under `checkedExceptions: true`, forgetting the `try` in `main` (or missing
the `ParseError` case while re-throwing the default) is a build failure. With
the option disabled, the syntax still parses without effect enforcement.

### 8.2 Higher-order code via `rethrows`

```ts
// Declared once in lib.d.ts:
//   map<U>(fn: (v: T, i: number) => U): U[] rethrows;

const parsed: Config[] = paths.map(readConfig); // inferred throws IOError | ParseError

function loadAll(paths: string[]): Config[] throws IOError | ParseError {
  return paths.map(readConfig); // propagated ✔
}
```

Without `rethrows`, `map` would either have to be `throws never` (rejecting any
throwing callback — unusable) or `throws unknown` (poisoning all callers).
`rethrows` gives it the callback's exact set.

### 8.3 Bridging to Effect.ts

The two systems compose cleanly, because a tracked `throws E` function is
exactly the raw material Effect needs:

```ts
import { Effect } from "effect";

// A `throws`-annotated function lifts into Effect's error channel with full type fidelity.
const program: Effect.Effect<Config, IOError | ParseError> =
  Effect.try({
    try: () => readConfig("./app.json"),
    catch: (e) => e as IOError | ParseError, // type is known — no `unknown` cast guesswork
  });
```

Because the compiler now knows `readConfig` throws `IOError | ParseError`, a
future `Effect.tryPromise`/`Effect.try` overload (or a codemod) can infer the
`E` channel automatically instead of forcing the author to restate it.

## 9. Lessons taken from Java (what we deliberately do differently)

| Java pain point | This proposal |
|---|---|
| `throws` is mandatory and hand-threaded | Inferred by default; required only at declaration boundaries (§6). |
| Breaks existing code when enabled | `false` by default; enabling it is an explicit migration to the sound contract (§6.4). |
| Higher-order functions are unusable (no `rethrows`) | `rethrows` inherits callback error sets (§4.4, §8.2). |
| `Exception` vs `RuntimeException` split is confusing | No checked/unchecked bifurcation. One structural error channel; opt out with `throws unknown`. |
| Encourages `catch (Exception e) {}` swallowing | Exhaustiveness + "declared but never raised" hints discourage empty catches; swallowing is explicit via a wide binding. |
| Nominal, class-based only | Structural — any type can be an error, unions compose naturally. |

## 10. Compatibility & rollout

### 10.1 Compiler-option placement

- Ship as a standalone boolean `checkedExceptions` option, **not** folded into
  TypeScript's existing `strict` boolean, for at
  least one major version, so `strict: true` upgrades don't break.
- Provide `--checkedExceptions` on the CLI, matching the JSON option.

### 10.2 `lib.d.ts` and ecosystem typings

- Unannotated `.d.ts` remains typeable, but calls through it contribute
  `unknown` until the declarations gain precise `throws` metadata (§6.4).
- The built-in `lib` files are upgraded incrementally: `JSON.parse` →
  `throws SyntaxError`, `Array.prototype.map` → `rethrows`, `fetch` /
  `Response.json` → their real rejection types, etc. This can land behind the
  same flag so it is inert until opted in.
- `Promise<T>` gains an optional tracked rejection slot. To stay
  backward-compatible we do **not** add a visible second type parameter to
  `Promise`; instead the rejection type is carried as an internal tracked
  channel populated by `async` inference and `throws` clauses, surfaced only
  through `await`/`catch`. (A `Promise<T, E>`-style public parameter is a
  possible future extension but is out of scope here to avoid a breaking
  `lib` change.)
- A `@throws {Type}` JSDoc tag lets hand-written and JS-mode declarations
  participate without new syntax.

### 10.3 Emit

**None.** `throws` / `rethrows` clauses are type-space only and are erased in
exactly the same pass that erases type annotations. No runtime, no downlevel
concern, no change to generated JS.

### 10.4 Declaration emit

`--declaration` includes the resolved throws set in emitted `.d.ts`:

```ts
export declare function readConfig(path: string): Config throws IOError | ParseError;
```

Inferred sets are materialized at the boundary just like inferred return types.

## 11. Implementation sketch (typescript-go)

A rough map onto the native compiler in this repository. This is indicative, not
a committed plan.

1. **Scanner/parser** (`internal/parser`, `internal/ast`)
   - Add a `throws` contextual keyword recognized after a return-type
     annotation in every function-like production; parse the following type into
     a new `ThrowsType` node hung off the signature.
   - Add the `rethrows` clause variant.
   - Gate *diagnostics* (not parsing) on the option so the syntax always parses.

2. **Types** (`internal/checker/types.go`)
   - Extend `Signature` with an optional `throwsType *Type` and a `rethrows`
     flag.
   - Represent an inferred/absent set distinctly from `throws never`.

3. **Checker** (`internal/checker/checker.go`, `flow.go`, `relater.go`)
   - Throws inference: extend the existing control-flow machinery in `flow.go`
     to accumulate a throws set per function, discharging it across `try`/`catch`
     the same way returns are discharged across branches.
   - Relation: in `relater.go`, add the covariant throws check to signature
     relation (§5.2).
   - `catch` binding type: compute from the enclosing `try` block's raised set
     instead of `unknown` when the option is enabled.
   - Emit build-blocking checked-exceptions diagnostics whenever the option is
     enabled.

4. **Options** (`internal/core/compileroptions.go`, `internal/tsoptions`)
   - Add `CheckedExceptions` as a boolean option,
     defaulting to `false`; wire the CLI flag and `tsconfig` key alongside the
     existing strict-family declarations in `declscompiler.go`.

5. **Declaration emit & language service** (`internal/checker/nodebuilder*.go`,
   `internal/ls`)
   - Serialize resolved throws sets into `.d.ts`.
   - Surface the error set on hover; add quick-fixes: "add `throws` clause",
     "surround with try/catch", "add missing catch case".

6. **Sound effect boundary** (`internal/checker/checkedexceptions.go`)
   - Normalize `any` effects and missing declaration metadata to `unknown`.
   - Make unions, overload selection, construction, property/proxy access,
     coercion, iteration, disposal, decorators/JSX, and inference cycles fail
     closed whenever the checker lacks a precise clause.
   - Prevent assignments and callable assertions from narrowing the throws set.
   - Maintain an adversarial baseline for every dynamic execution door; adding
     an ECMAScript operation requires classifying its abrupt completions.

7. **Async and callback timing**
   - Carry a hidden rejection effect from an async signature to immediate
     `await`, and reject floating promise effects. A synchronous catch around a
     promise-producing call never discharges the rejection.
   - Add invocation timing to callback parameters: `rethrows` means proven
     synchronous/non-escaping invocation; an escaping/event callback must
     handle its own throws and rejection effects. Missing timing metadata is
     escaping whenever checked exceptions are enabled.
   - Update `Promise.then`/`catch`/`finally`, timers, event targets, Node-style
     callbacks, and iterator callbacks only after each declaration states how
     callback effects transform.

8. **Pattern-matching catch integration** (gated on TC39 syntax)
   - Parse `catch match` only behind an experimental syntax flag while the
     proposal remains unsettled; keep ordinary `catch` unchanged.
   - Reuse the pattern proposal's runtime matcher and control-flow narrowing for
     each arm. Compute the handled type `H`, propagate `Exclude<E, H>` on the
     implicit unmatched rethrow, and add matcher evaluation's own effect.
   - Require a wildcard/default arm to discharge `unknown`; finite structural
     patterns never prove an unknown set exhaustive.
   - Test arm order, guards, custom matchers that throw, getter/proxy effects,
     unmatched rethrow, `finally` override, async `await`, and declaration emit.

9. **Proof and rollout**
   - Run parser/emitter baselines, relation tests, adversarial effect-door tests,
     cold full-suite builds, and browser playground cases with the option on and off.
   - Fuzz nested `try`/`catch`/`finally`, recursive call graphs, overloads,
     unions, assertions, async chains, and match arms. A checked-exceptions crash,
     missing diagnostic, or effect-narrowing path is a release blocker.
   - Keep the opt-in feature experimental until standard-library annotations
     and callback timing contracts cover a useful ecosystem slice; never
     silently weaken `unknown` to gain compatibility.

## 12. Reference implementation (this repository)

This repository carries a working implementation of the proposal's core. What
is implemented, and where it deliberately narrows the full design:

**Implemented**

- `throws T` clause syntax on function declarations/expressions, arrows,
  methods, constructors, getters/setters, call and construct signatures, and
  function and constructor type nodes; `throws` is a contextual keyword
  recognized only on the same line as the preceding token, so existing members
  named `throws` keep parsing.
- The boolean `checkedExceptions` option, wired through
  tsconfig, the CLI, and build info; diagnostics TS100021–TS100028 are emitted
  as build-blocking errors when it is enabled.
- Handle-or-declare enforcement (§4.3), including top-level code, with
  raises in `catch`/`finally` blocks correctly not discharged by their own
  `try`.
- Throws inference for unannotated functions with bodies (§6.1), transitive
  through call sites and cycle-safe; inferred raises surface at call sites
  (including IIFEs) instead of inside the unannotated function.
- Typed `catch` (§4.2): the union of what the `try` block can raise.
- Covariant throws in signature assignability (§5.2) with a dedicated
  elaboration message, including contextually-typed arrows checked against
  `throws never` targets.
- Full erasure from JS emit; preservation in declaration emit (both the
  syntactic path and signatures rebuilt from types), hover, and the `throws
  any`/`throws unknown` top effect (§7.3).
- Type parameters are in scope in the clause (`throws T` on generics), and the
  clause instantiates with the signature.
- Enabled checked-exceptions analysis treats ambient/legacy declarations without clauses as `unknown`,
  normalizes `throws any` to `unknown`, includes unknown effects in enforcement
  and catch typing, prevents assertions or `any` assignments from manufacturing
  callable/property effect proofs, checks function/method/constructor overload
  implementations, and conservatively treats
  unresolved structural property access, construction, class/module
  evaluation, coercion, iteration, destructuring, disposal, JSX, and decorators
  as unknown effects.
- Ordinary local code is analyzed rather than blanket-poisoned: inert class
  declarations, local default constructors, concrete data fields, string
  `length`, and primitive arithmetic are proven safe. Getter and setter bodies
  have distinct inferred effects, and those effects participate in structural
  assignability so an accessor cannot masquerade as a non-throwing data field.
- Async checks distinguish immediate `await`, direct promise propagation,
  floating rejections, and callbacks that may escape the caller's control-flow
  boundary.

**Deliberate narrowings (future work)**

- `rethrows` (§4.4) syntax is not implemented. The checker still fails closed
  for an unannotated callback parameter and infers `unknown` for wrappers whose
  callback effect cannot be expressed, but it cannot yet preserve a precise
  callback-dependent effect. Function-valued arguments that may throw are
  rejected as potentially escaping; this is sound but intentionally rejects
  synchronous combinators until they can declare the timing contract.
- Until parameter-level timing contracts land, the implementation recursively
  examines option objects, collections, index signatures, unions, and generic
  capabilities for executable code. Throwing or imprecise callbacks,
  constructors, accessors, and returned capabilities are rejected at the call
  site because they may escape. This is intentionally conservative; `rethrows`
  and explicit non-escaping contracts can later admit synchronous combinators
  without weakening the sound boundary.
- Typed `catch` requires `checkedExceptions: true`. With the option disabled, program types
  are byte-for-byte what they are today; the proposal's "typed catch in the
  editor even when off" would change types under a no-diagnostics setting.
- The assignability rule runs whenever checked exceptions are enabled and a
  failed relation is a hard type error.
- Ambient construct signatures without clauses, structural property signatures, static blocks,
  computed/decorated class elements, tagged templates, and JSX contribute
  `unknown` until their declarations carry a precise effect contract. Visible
  local constructors, accessors, and field initializers are inferred.
- Static imports are currently rejected as `unknown` module-initialization
  effects. A practical multi-module project needs declaration emit and
  package metadata for module initialization effects before this can become
  precise.
- `throws this` is not instantiated at call sites; the clause resolves to the
  declaring class's `this` type.
- `catch match`/`catch (e is pattern)` is specified as a sound future
  integration, but parser, emitter, and control-flow support are not yet
  implemented because TC39 currently lists catch integration as a possible
  future enhancement.
- Throws *inference* treats a rethrown catch variable as the catch clause's
  full union rather than its flow-narrowed type (enforcement against explicit
  clauses uses the precise narrowed type). An unannotated function that
  narrows before rethrowing may therefore be inferred with a wider throws set
  than it can actually raise — an over-approximation, never a missed error.

## 13. Open questions

1. **Constructors, getters, field initializers, and destructors** — do we track
   throws through property initializers and getter access? (Proposed: yes for
   getters, since access is a call; class-field initializers fold into the
   constructor's set.)
2. **`throws` on function *types* vs. only declarations** — the proposal allows
   it on both; confirm the surface syntax reads well in complex positions
   (e.g. `Array<(x: T) => U throws E>`).
3. **Exhaustiveness of `catch` re-throws** — how aggressively to infer that a
   `catch` that conditionally re-throws narrows the propagated set.
4. **Interaction with `noImplicitAny`** — should an inferred `throws unknown` at
   a legacy boundary key off `noImplicitAny` rather than a bespoke sub-option?
5. **Should `throws` participate in variance annotations** on generic type
   parameters, or is signature-level covariance sufficient?
6. **Non-error throws** — code that `throw`s strings/numbers. Tracked as their
   literal/primitive types; no requirement that thrown values extend `Error`
   (consistent with today's permissiveness).

## 14. Alternatives considered

- **Result types in `lib` only** (a built-in `Result<T, E>` + `?`-style
  operator). Rejected as the *primary* mechanism because it does not describe
  the existing `throw`-based ecosystem and forces call-site rewrites — though it
  remains an attractive *complementary* future feature and composes with this
  one.
- **Lint-rule-only (ESLint/tsc plugin).** Rejected because accurate throws
  inference and typed `catch` require the checker's type information and
  control-flow graph; a syntactic lint cannot do §5–§6 soundly.
- **Mandatory clauses (pure Java).** Rejected for the ergonomic reasons in §9.

---

### Appendix A: Grammar additions (informal)

```
FunctionBody-preceding:
    ReturnTypeAnnotation? ThrowsClause?

ThrowsClause:
    'throws' Type
    'rethrows'
```

`throws` and `rethrows` are contextual keywords, permitted only in the throws-
clause position, so no identifier named `throws` is broken.

### Appendix B: Option reference

| Option | Type | Default | Effect |
|---|---|---|---|
| `checkedExceptions` | `boolean` | `false` | Enable fail-closed checked-exception effects. |
