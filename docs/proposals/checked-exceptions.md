# Proposal: Checked Exceptions & First-Class Error Tracking for TypeScript

- **Status:** Draft / Request for Comments
- **Champion:** _(unassigned)_
- **Target:** TypeScript language + `tsc` (including the native Go port, `typescript-go`)
- **Feature flag:** `checkedExceptions` (`"off" | "warning" | "error"`, default `"off"`)

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
gated behind a single compiler option, `checkedExceptions`, whose value —
`"off"`, `"warning"`, or `"error"` — lets teams adopt it as a no-op, a lint, or
a hard build gate.

The design goal is to bring the *safety* of [Effect.ts](https://effect.website/)'s
typed error channel to *idiomatic* TypeScript — using ordinary `throw` / `try` /
`catch`, not a wrapper monad — while keeping the entire feature opt-in and
gradually adoptable.

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

1. **Opt-in, always.** With `checkedExceptions: "off"` (the default), the new
   syntax parses and is available for tooling, but produces **zero** new
   diagnostics. Existing code and existing `tsconfig` files compile unchanged.
2. **Gradual.** You can annotate one function, one file, or one package at a
   time. Un-annotated code is treated permissively (see §6.4).
3. **Idiomatic.** Uses `throw` / `try` / `catch`. No new runtime, no monad, no
   boxing. **Zero emit impact** — throws clauses are erased like type
   annotations.
4. **Structural, inferred, and sound-leaning.** Consistent with TypeScript:
   error sets are inferred where possible and behave structurally, not
   nominally.
5. **One knob.** A single option with three severities, mirroring how the
   ecosystem already reasons about `off` / `warn` / `error`.

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

### 6.4 The meaning of "no clause" under each mode

Because the feature is gradual, an *absent* clause on a **declared** boundary
(e.g. an imported `.d.ts` written before this feature, or third-party types)
must not spuriously break builds. The resolution:

| Context | Interpretation of a missing `throws` clause |
|---|---|
| Local function with a visible body | **Inferred** from the body (§6.1). |
| Ambient / `.d.ts` / no visible body, `checkedExceptions` off | Ignored. |
| Ambient / `.d.ts` / no visible body, warning or error | Treated as `throws unknown` **only when its result is used in a checked context**, and reported at a distinct, separately-suppressible diagnostic (§7.3), so legacy typings degrade to a warning rather than a hard failure. |

A future `@throws` JSDoc tag and a `lib` upgrade (§10.2) let ambient
declarations opt into precise sets over time.

## 7. Diagnostics & the `checkedExceptions` option

### 7.1 The option

```jsonc
// tsconfig.json
{
  "compilerOptions": {
    "checkedExceptions": "error" // "off" (default) | "warning" | "error"
  }
}
```

- **`"off"`** — default. Syntax parses; language service still offers typed
  `catch` and quick-fixes, but the checker emits no throws-related diagnostics.
  Fully backward compatible.
- **`"warning"`** — every unhandled/undeclared error is reported at
  `CategoryWarning`. Ideal for incremental adoption in an existing codebase:
  surfaces the problem without blocking CI.
- **`"error"`** — reported at `CategoryError`; unhandled errors fail the build,
  giving Java-style hard enforcement.

The tri-state maps directly onto the compiler's existing diagnostic categories
(`CategoryWarning` / `CategoryError` in `internal/diagnostics/diagnostics.go`),
so no new severity infrastructure is needed — the same diagnostic is emitted at
the category selected by the option.

A matching CLI flag (`--checkedExceptions error`) and a `strict`-family
consideration are discussed in §10.1. Note it is intentionally **not** folded
into `strict` by default, to avoid breaking every strict codebase on upgrade.

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

### 7.3 Degradation diagnostics (separately suppressible)

- **TSxxx4: "'{0}' comes from a declaration without throws information and is
  treated as possibly-throwing."** — the legacy-typings escape hatch from §6.4.
  Emitted at warning severity even in `"error"` mode unless the stricter
  `checkedExceptionsStrictLegacy` sub-option is set. This is what prevents the
  Java "the whole ecosystem breaks at once" failure.

### 7.4 Suppression & escape hatches

- `throws unknown` (or `throws any`) on a function explicitly opts it out of
  precise tracking — it becomes a "throws anything" boundary and its callers are
  never forced to handle a specific type (they may `catch (e: unknown)` as
  today). This is the deliberate "I give up on precision here" marker.
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

Under `checkedExceptions: "error"`, forgetting the `try` in `main` (or missing
the `ParseError` case while re-throwing the default) is a build failure. Under
`"warning"`, it is a yellow squiggle. Under `"off"`, the code above still
type-checks and you *still* get `e: IOError | ParseError` in the editor.

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
| Breaks all existing code when enabled | `"off"` by default; legacy typings degrade to warnings (§6.4, §7.3). |
| Higher-order functions are unusable (no `rethrows`) | `rethrows` inherits callback error sets (§4.4, §8.2). |
| `Exception` vs `RuntimeException` split is confusing | No checked/unchecked bifurcation. One structural error channel; opt out with `throws unknown`. |
| Encourages `catch (Exception e) {}` swallowing | Exhaustiveness + "declared but never raised" hints discourage empty catches; swallowing is explicit via a wide binding. |
| Nominal, class-based only | Structural — any type can be an error, unions compose naturally. |

## 10. Compatibility & rollout

### 10.1 Compiler-option placement

- Ship as a standalone `checkedExceptions` tri-state, **not** in `strict`, for at
  least one major version, so `strict: true` upgrades don't break.
- Provide `--checkedExceptions` on the CLI, matching the JSON option.
- Optionally add a companion `checkedExceptionsStrictLegacy: boolean` (§7.3) for
  teams that want to forbid the legacy-typings degradation.

### 10.2 `lib.d.ts` and ecosystem typings

- Un-annotated `.d.ts` (all of DefinitelyTyped today) keeps working via §6.4.
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
   - Emit TSxxx1–TSxxx4 at the category selected by the option, reusing
     `CategoryWarning` / `CategoryError`.

4. **Options** (`internal/core/compileroptions.go`, `internal/tsoptions`)
   - Add `CheckedExceptions` as a string/enum tri-state option with parsing,
     defaulting to `off`; wire the CLI flag and `tsconfig` key alongside the
     existing strict-family declarations in `declscompiler.go`.

5. **Declaration emit & language service** (`internal/checker/nodebuilder*.go`,
   `internal/ls`)
   - Serialize resolved throws sets into `.d.ts`.
   - Surface the error set on hover; add quick-fixes: "add `throws` clause",
     "surround with try/catch", "add missing catch case".

## 12. Open questions

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

## 13. Alternatives considered

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
| `checkedExceptions` | `"off" \| "warning" \| "error"` | `"off"` | Severity of unhandled/undeclared tracked errors. |
| `checkedExceptionsStrictLegacy` | `boolean` | `false` | Escalate the "declaration without throws info" degradation (TSxxx4) to full severity instead of warning. |
