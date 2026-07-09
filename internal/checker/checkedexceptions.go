package checker

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
)

// Checked exceptions (`checkedExceptions` compiler option).
//
// A function may declare the errors it can throw with a `throws T` clause. When
// the option is set to "warning" or "error", the checker enforces that every
// tracked error raised in a body — a `throw` statement, or a call to a function
// whose signature declares (or, for unannotated functions with visible bodies,
// infers) a throws type — is either caught by an enclosing `try` statement with
// a `catch` clause, or declared by the enclosing function's own `throws` clause.
// Code outside of any function must catch all tracked errors.
//
// Enforcement summary:
//   - A function with a `throws` clause: every raise in its body must be caught
//     or assignable to the clause type. `throws never` declares that nothing
//     escapes.
//   - A function without a clause: raises silently propagate; its throws type is
//     inferred from its body and surfaces at *its* call sites instead.
//   - Top level: every raise must be caught.
//   - A callee that declares `throws any` or `throws unknown` opts out of
//     enforcement at its call sites (the escape hatch for imprecise code), but
//     still contributes to `catch` variable types.
//   - Constructors, accessors, static blocks, and class field initializers are
//     not tracked in this initial implementation.
//
// The option also types `catch` variables: an unannotated catch binding's type
// becomes the union of the error types the `try` block can raise, when that set
// is known and non-empty; otherwise it stays `unknown`/`any` as before.

func (c *Checker) checkedExceptionsEnabled() bool {
	return c.compilerOptions.CheckedExceptions >= core.CheckedExceptionsModeWarning
}

// errorCheckedExceptions reports a checked-exceptions diagnostic at the category
// selected by the option ("warning" downgrades the message's error category).
func (c *Checker) errorCheckedExceptions(location *ast.Node, message *diagnostics.Message, args ...any) {
	diagnostic := NewDiagnosticForNode(location, message, args...)
	if c.compilerOptions.CheckedExceptions == core.CheckedExceptionsModeWarning {
		diagnostic.SetCategory(diagnostics.CategoryWarning)
	}
	c.addDiagnostic(diagnostic)
}

// throwsClauseNode returns the syntactic `throws` clause of a function-like
// declaration, or nil when the declaration has none (or cannot carry one).
func throwsClauseNode(fn *ast.Node) *ast.TypeNode {
	if fn == nil {
		return nil
	}
	if data := fn.FunctionLikeData(); data != nil {
		return data.ThrowsType
	}
	return nil
}

// canInferThrows reports whether a declaration's throws type may be inferred
// from its body when it lacks an explicit clause.
func canInferThrows(decl *ast.Node) bool {
	switch decl.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration:
		return decl.Body() != nil
	}
	return false
}

// getDeclaredThrowsTypeOfSignature returns the type of the signature's explicit
// `throws` clause, instantiated like the rest of the signature, or nil when the
// signature carries no clause.
func (c *Checker) getDeclaredThrowsTypeOfSignature(sig *Signature) *Type {
	return c.getThrowsTypeOfSignatureWorker(sig, false /*includeInferred*/)
}

// getThrowsTypeOfSignature returns the signature's throws type: the explicit
// clause when present, or the type inferred from the declaration's body for
// unannotated functions. Returns nil for untracked signatures (no clause and no
// body to infer from), which callers must treat permissively.
func (c *Checker) getThrowsTypeOfSignature(sig *Signature) *Type {
	return c.getThrowsTypeOfSignatureWorker(sig, true /*includeInferred*/)
}

func (c *Checker) getThrowsTypeOfSignatureWorker(sig *Signature, includeInferred bool) *Type {
	if sig == nil {
		return nil
	}
	if sig.target != nil {
		base := c.getThrowsTypeOfSignatureWorker(sig.target, includeInferred)
		if base == nil {
			return nil
		}
		return c.instantiateType(base, sig.mapper)
	}
	if sig.composite != nil {
		// A composite (union/intersection) signature raises the union of its
		// tracked constituents' throws types. Untracked constituents contribute
		// nothing (they are permissive everywhere else too); if none are
		// tracked, the composite is untracked.
		var throwsTypes []*Type
		for _, constituent := range sig.composite.signatures {
			if t := c.getThrowsTypeOfSignatureWorker(constituent, includeInferred); t != nil {
				throwsTypes = append(throwsTypes, t)
			}
		}
		if len(throwsTypes) == 0 {
			return nil
		}
		return c.getUnionType(throwsTypes)
	}
	decl := sig.declaration
	if decl == nil {
		return nil
	}
	if clause := throwsClauseNode(decl); clause != nil {
		return c.getTypeFromTypeNode(clause)
	}
	if includeInferred && c.checkedExceptionsEnabled() && canInferThrows(decl) {
		return c.getInferredThrowsTypeOfFunction(decl)
	}
	return nil
}

// throwsInferenceState tracks an in-flight throws inference. Recursion cycles
// mean a single pass can compute incomplete unions (a recursive edge sees only
// what has been discovered so far), so the outermost query iterates to a
// fixpoint before committing results to the permanent cache: raise sets only
// grow between iterations, and union types are interned, so iteration stops as
// soon as nothing changes.
type throwsInferenceState struct {
	provisional map[*ast.Node]*Type
	inProgress  map[*ast.Node]bool
	computed    map[*ast.Node]bool // computed during the current iteration
	changed     bool
}

// getInferredThrowsTypeOfFunction infers the throws type of an unannotated
// function: the union of everything its body and parameter initializers can
// raise that no internal try...catch discharges. Recursive call graphs are
// resolved by fixpoint iteration. Returns nil when nothing tracked is raised.
func (c *Checker) getInferredThrowsTypeOfFunction(decl *ast.Node) *Type {
	if c.inferredThrowsTypes == nil {
		c.inferredThrowsTypes = make(map[*ast.Node]*Type)
	}
	if cached, ok := c.inferredThrowsTypes[decl]; ok {
		return cached
	}
	if state := c.throwsInference; state != nil {
		// Nested query inside an ongoing inference.
		if state.inProgress[decl] {
			// Recursive edge: contribute the previous iteration's value; the
			// outer fixpoint loop re-runs until this stabilizes.
			return state.provisional[decl]
		}
		if state.computed[decl] {
			return state.provisional[decl]
		}
		return c.inferThrowsWorker(decl, state)
	}
	state := &throwsInferenceState{
		provisional: make(map[*ast.Node]*Type),
		inProgress:  make(map[*ast.Node]bool),
		computed:    make(map[*ast.Node]bool),
	}
	c.throwsInference = state
	// Reset via defer so a panic anywhere below (signature resolution, union
	// construction, ...) cannot leave the checker stuck in the in-flight state.
	defer func() { c.throwsInference = nil }()
	var result *Type
	// Raise sets grow monotonically, so this terminates; the cap is a backstop
	// against pathological cycle shapes, accepting the partial result instead
	// of iterating further.
	for range 100 {
		state.changed = false
		clear(state.computed)
		result = c.inferThrowsWorker(decl, state)
		if !state.changed {
			break
		}
	}
	for d, t := range state.provisional {
		c.inferredThrowsTypes[d] = t
	}
	return result
}

func (c *Checker) inferThrowsWorker(decl *ast.Node, state *throwsInferenceState) *Type {
	state.inProgress[decl] = true
	var raised []*Type
	for _, parameter := range decl.Parameters() {
		raised = append(raised, c.collectRaisedTypes(parameter.Initializer())...)
	}
	raised = append(raised, c.collectRaisedTypes(decl.Body())...)
	delete(state.inProgress, decl)
	state.computed[decl] = true
	var result *Type
	if len(raised) != 0 {
		result = c.getUnionType(raised)
	}
	if state.provisional[decl] != result {
		state.changed = true
		state.provisional[decl] = result
	}
	return result
}

// getThrowsTypeOfCall returns the tracked throws type of a call or new
// expression's resolved signature, or nil when the callee is untracked.
func (c *Checker) getThrowsTypeOfCall(node *ast.Node) *Type {
	signature := c.getResolvedSignature(node, nil /*candidatesOutArray*/, CheckModeNormal)
	return c.getThrowsTypeOfSignature(signature)
}

// collectRaisedTypes gathers the types a statement subtree can raise past any
// try...catch statements it contains. Nested function bodies do not raise
// (calling them does); a try block whose statement has a catch clause is fully
// discharged, though its catch and finally blocks still raise outward.
func (c *Checker) collectRaisedTypes(node *ast.Node) []*Type {
	var raised []*Type
	var visit func(node *ast.Node) bool
	add := func(t *Type) {
		if t != nil && t != c.errorType && t.flags&TypeFlagsNever == 0 {
			raised = append(raised, t)
		}
	}
	visit = func(node *ast.Node) bool {
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindClassStaticBlockDeclaration, ast.KindClassDeclaration, ast.KindClassExpression:
			return false
		case ast.KindTryStatement:
			t := node.AsTryStatement()
			if t.CatchClause != nil {
				visit(t.CatchClause)
			} else {
				visit(t.TryBlock)
			}
			if t.FinallyBlock != nil {
				visit(t.FinallyBlock)
			}
			return false
		case ast.KindCallExpression, ast.KindNewExpression:
			add(c.getThrowsTypeOfCall(node))
		case ast.KindThrowStatement:
			expr := node.Expression()
			if catchClause := c.getCatchClauseOfRethrownVariable(expr); catchClause != nil {
				// Rethrow of an unannotated catch variable: contribute the catch
				// clause's own computed union instead of resolving the variable's
				// type. Resolving it here can re-enter the checker's symbol type
				// resolution mid-inference (mutually recursive rethrow wrappers)
				// and produce a spurious circularity error.
				add(c.getCatchClauseThrowsType(catchClause))
			} else {
				add(c.getTypeOfExpression(expr))
			}
		}
		return node.ForEachChild(visit)
	}
	if node != nil {
		visit(node)
	}
	return raised
}

// getCatchClauseOfRethrownVariable returns the catch clause whose unannotated
// variable the expression rethrows, or nil.
func (c *Checker) getCatchClauseOfRethrownVariable(expr *ast.Node) *ast.Node {
	if expr == nil || !ast.IsIdentifier(expr) {
		return nil
	}
	symbol := c.getResolvedSymbol(expr)
	if symbol == nil || symbol == c.unknownSymbol || symbol.ValueDeclaration == nil {
		return nil
	}
	declaration := symbol.ValueDeclaration
	if !ast.IsVariableDeclaration(declaration) || declaration.Parent == nil || declaration.Parent.Kind != ast.KindCatchClause {
		return nil
	}
	if declaration.Type() != nil {
		return nil
	}
	return declaration.Parent
}

// getCatchClauseThrowsType computes the type for an unannotated catch variable:
// the union of the error types the corresponding try block can raise. Returns
// nil when nothing tracked is raised (the variable then defaults to
// unknown/any as usual).
func (c *Checker) getCatchClauseThrowsType(catchClause *ast.Node) *Type {
	if c.catchClauseThrowsTypes == nil {
		c.catchClauseThrowsTypes = make(map[*ast.Node]*Type)
	}
	if cached, ok := c.catchClauseThrowsTypes[catchClause]; ok {
		return cached
	}
	// Mark in-progress; a cyclic query contributes nothing.
	c.catchClauseThrowsTypes[catchClause] = nil
	var result *Type
	if tryStatement := catchClause.Parent; tryStatement != nil && tryStatement.Kind == ast.KindTryStatement {
		raised := c.collectRaisedTypes(tryStatement.AsTryStatement().TryBlock)
		if len(raised) != 0 {
			result = c.getUnionType(raised)
		}
	}
	if c.throwsInference != nil {
		// Computed from provisional inference results: usable now, but not safe
		// to cache past the enclosing fixpoint iteration.
		delete(c.catchClauseThrowsTypes, catchClause)
		return result
	}
	c.catchClauseThrowsTypes[catchClause] = result
	return result
}

// checkCheckedExceptionsForFile enforces handle-or-declare for every tracked
// raise in the file. Runs as a standalone pass after the file is otherwise
// checked, so all call signatures are already resolved and cached.
func (c *Checker) checkCheckedExceptionsForFile(sourceFile *ast.SourceFile) {
	// enclosingFn is the nearest enclosing function-like (or class, for code in
	// field initializers and similar positions, which this pass does not
	// enforce); nil means top-level module code. tryDepth counts enclosing
	// try-with-catch blocks within the current function.
	var enclosingFn *ast.Node
	tryDepth := 0

	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindClassStaticBlockDeclaration, ast.KindClassDeclaration, ast.KindClassExpression:
			savedFn, savedTryDepth := enclosingFn, tryDepth
			enclosingFn, tryDepth = node, 0
			node.ForEachChild(visit)
			enclosingFn, tryDepth = savedFn, savedTryDepth
			return false
		case ast.KindTryStatement:
			t := node.AsTryStatement()
			if t.CatchClause != nil {
				tryDepth++
				visit(t.TryBlock)
				tryDepth--
				visit(t.CatchClause)
			} else {
				visit(t.TryBlock)
			}
			if t.FinallyBlock != nil {
				visit(t.FinallyBlock)
			}
			return false
		case ast.KindCallExpression, ast.KindNewExpression:
			if tryDepth == 0 {
				c.checkRaisedType(node, c.getThrowsTypeOfCall(node), enclosingFn, true /*isCall*/)
			}
		case ast.KindThrowStatement:
			if tryDepth == 0 {
				c.checkRaisedType(node, c.getTypeOfExpression(node.Expression()), enclosingFn, false /*isCall*/)
			}
		}
		return node.ForEachChild(visit)
	}
	sourceFile.AsNode().ForEachChild(visit)
}

// checkRaisedType reports a raise that no enclosing try...catch discharges,
// unless the enclosing function declares a covering `throws` clause or is an
// unannotated function (whose raises propagate via inference instead).
func (c *Checker) checkRaisedType(site *ast.Node, raised *Type, enclosingFn *ast.Node, isCall bool) {
	if raised == nil || raised == c.errorType || raised.flags&(TypeFlagsNever|TypeFlagsAnyOrUnknown) != 0 {
		// Untracked, empty, or explicitly opted out (`throws any`/`throws unknown`).
		// `throw` of an any/unknown value is likewise not enforced.
		return
	}
	if enclosingFn == nil {
		message := core.IfElse(isCall,
			diagnostics.Call_may_throw_0_which_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_handle_all_checked_exceptions,
			diagnostics.Thrown_value_of_type_0_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_handle_all_checked_exceptions)
		c.errorCheckedExceptions(site, message, c.TypeToString(raised))
		return
	}
	clause := throwsClauseNode(enclosingFn)
	if clause == nil {
		// Unannotated function (or a construct that cannot declare a clause):
		// the raise propagates through the function's inferred throws type.
		return
	}
	if !c.isTypeAssignableTo(raised, c.getTypeFromTypeNode(clause)) {
		message := core.IfElse(isCall,
			diagnostics.Call_may_throw_0_which_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_function_s_throws_clause,
			diagnostics.Thrown_value_of_type_0_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_function_s_throws_clause)
		c.errorCheckedExceptions(site, message, c.TypeToString(raised))
	}
}
