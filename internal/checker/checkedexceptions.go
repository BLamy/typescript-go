package checker

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/tspath"
)

// Checked exceptions (`checkedExceptions` compiler option).
//
// A function may declare the errors it can throw with a `throws T` clause. When
// the option is true, the checker enforces that every
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
//   - A callee that declares `throws any` or `throws unknown` has the top
//     `unknown` effect and must be caught or propagated.
//   - Enabled analysis fails closed for dynamic property access, construction, class
//     evaluation, coercion/iteration, binding patterns, and other implicit
//     operations that can execute user or host code without a precise clause.
//
// The option also types `catch` variables: an unannotated catch binding's type
// becomes the union of the error types the `try` block can raise, when that set
// is known and non-empty; otherwise it stays `unknown`/`any` as before.

func (c *Checker) checkedExceptionsEnabled() bool {
	return c.compilerOptions.CheckedExceptions.IsTrue()
}

func (c *Checker) checkedExceptionsFailClosed() bool {
	return c.checkedExceptionsEnabled()
}

// normalizeThrowsType turns TypeScript's unsound `any` top type into the
// honest `unknown` top effect whenever checked exceptions are enabled.
func (c *Checker) normalizeThrowsType(t *Type) *Type {
	if c.checkedExceptionsFailClosed() && t != nil && t.flags&TypeFlagsAny != 0 {
		return c.unknownType
	}
	return t
}

// errorCheckedExceptions reports a build-blocking checked-exceptions diagnostic.
func (c *Checker) errorCheckedExceptions(location *ast.Node, message *diagnostics.Message, args ...any) {
	diagnostic := NewDiagnosticForNode(location, message, args...)
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
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration,
		ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
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
// unannotated functions. Returns nil for signatures whose visible body proves
// no escaping effect, and for all signatures while the feature is disabled.
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
		// constituents' throws types. When checking is enabled, a constituent
		// without enough declaration information contributes `unknown` below.
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
		if c.checkedExceptionsFailClosed() {
			// Synthetic/native signatures without a source declaration are another
			// unknown boundary. Absence of syntax is not evidence of purity.
			return c.unknownType
		}
		return nil
	}
	if clause := throwsClauseNode(decl); clause != nil {
		return c.normalizeThrowsType(c.getTypeFromTypeNode(clause))
	}
	if includeInferred && c.checkedExceptionsEnabled() && canInferThrows(decl) {
		return c.getInferredThrowsTypeOfFunction(decl)
	}
	if c.checkedExceptionsFailClosed() && !canInferThrows(decl) {
		// An ambient/declaration-only signature without a clause is an unknown
		// boundary. DefinitelyTyped and legacy lib declarations therefore remain
		// usable without pretending that they cannot throw.
		return c.unknownType
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
	converged := false
	// Raise sets grow monotonically, so this terminates; the cap is a backstop
	// against pathological cycle shapes.
	for range 100 {
		state.changed = false
		clear(state.computed)
		result = c.inferThrowsWorker(decl, state)
		if !state.changed {
			converged = true
			break
		}
	}
	if !converged && c.checkedExceptionsFailClosed() {
		// The cap is only a termination guard, never permission to publish an
		// incomplete effect. A graph that does not stabilize becomes the honest
		// top effect in fail-closed mode.
		result = c.unknownType
		for d := range state.provisional {
			state.provisional[d] = c.unknownType
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
		raised = append(raised, c.collectRaisedTypes(parameter.Name())...)
		raised = append(raised, c.collectRaisedTypes(parameter.Initializer())...)
	}
	raised = append(raised, c.collectRaisedTypes(decl.Body())...)
	raised = append(raised, c.collectReturnedPromiseEffects(decl)...)
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
	throwsType := c.getThrowsTypeOfSignature(signature)
	if c.checkedExceptionsFailClosed() {
		if node.Kind == ast.KindNewExpression {
			if classEffect, ok := c.getLocalClassConstructionThrowsType(node); ok {
				target := node.Expression()
				if target != nil && (target.Kind == ast.KindPropertyAccessExpression || target.Kind == ast.KindElementAccessExpression) {
					// Resolving `new namespace.C()` can execute a getter or Proxy
					// trap before the locally visible constructor runs.
					return c.unionThrowsTypes(classEffect, c.getPropertyAccessThrowsType(target))
				}
				return classEffect
			}
		}
		target := node.Expression()
		if target != nil && (target.Kind == ast.KindPropertyAccessExpression || target.Kind == ast.KindElementAccessExpression) {
			return c.unionThrowsTypes(throwsType, c.getPropertyAccessThrowsType(target))
		}
	}
	return throwsType
}

func (c *Checker) isUnsupportedProxyFactoryCall(node *ast.Node) bool {
	signature := c.getResolvedSignature(node, nil /*candidatesOutArray*/, CheckModeNormal)
	if signature == nil || signature.declaration == nil {
		return false
	}
	sourceFile := ast.GetSourceFileOfNode(signature.declaration)
	return sourceFile != nil && c.program.IsSourceFileDefaultLibrary(sourceFile.Path()) &&
		tspath.GetBaseFileName(sourceFile.FileName()) == "lib.es2015.proxy.d.ts"
}

func (c *Checker) unionThrowsTypes(types ...*Type) *Type {
	var present []*Type
	for _, t := range types {
		if t != nil && t != c.errorType && t.flags&TypeFlagsNever == 0 {
			present = append(present, c.normalizeThrowsType(t))
		}
	}
	if len(present) == 0 {
		return nil
	}
	return c.getUnionType(present)
}

// getPropertyAccessThrowsType models the lookup itself, separately from a
// subsequent method call. Concrete data properties are inert; visible
// accessors contribute the effect inferred from their bodies. Structural or
// ambient property contracts cannot distinguish a data slot from a getter (or
// a proxy trap), so they remain unknown until property effects are expressible
// in declarations.
func (c *Checker) getPropertyAccessThrowsType(node *ast.Node) *Type {
	if node == nil || (node.Kind != ast.KindPropertyAccessExpression && node.Kind != ast.KindElementAccessExpression) {
		return nil
	}
	baseType := c.getTypeOfExpression(node.Expression())
	if baseType == nil || baseType.flags&TypeFlagsAnyOrUnknown != 0 {
		return c.unknownType
	}

	var propertyName string
	if node.Kind == ast.KindPropertyAccessExpression {
		propertyName = node.Name().Text()
	} else {
		argument := node.AsElementAccessExpression().ArgumentExpression
		if argument == nil || argument.Kind != ast.KindStringLiteral {
			return c.unknownType
		}
		propertyName = argument.Text()
	}

	// String values have an own, non-accessor length slot. Reading it neither
	// consults a mutable prototype nor performs user coercion.
	if propertyName == "length" && baseType.flags&TypeFlagsStringLike != 0 {
		return nil
	}

	property := c.getPropertyOfType(baseType, propertyName)
	if property == nil || len(property.Declarations) == 0 {
		return c.unknownType
	}
	assignmentKind := getAssignmentTargetKind(node)
	reads := assignmentKind != AssignmentKindDefinite
	writes := assignmentKind != AssignmentKindNone
	return c.getPropertySymbolAccessThrowsType(property, reads, writes)
}

func (c *Checker) getPropertySymbolAccessThrowsType(property *ast.Symbol, reads bool, writes bool) *Type {
	if property == nil || len(property.Declarations) == 0 {
		return c.unknownType
	}
	var effects []*Type
	hasGetter := false
	hasSetter := false
	for _, declaration := range property.Declarations {
		switch declaration.Kind {
		case ast.KindGetAccessor:
			hasGetter = true
			if !reads {
				continue
			}
			effect := c.getThrowsTypeOfSignature(c.getSignatureFromDeclaration(declaration))
			if effect == nil && declaration.Body() == nil {
				effect = c.unknownType
			}
			effects = append(effects, effect)
		case ast.KindSetAccessor:
			hasSetter = true
			if !writes {
				continue
			}
			effect := c.getThrowsTypeOfSignature(c.getSignatureFromDeclaration(declaration))
			if effect == nil && declaration.Body() == nil {
				effect = c.unknownType
			}
			effects = append(effects, effect)
		case ast.KindPropertyDeclaration, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment,
			ast.KindMethodDeclaration, ast.KindClassDeclaration, ast.KindClassExpression:
			// These declarations install concrete data properties. Initializer and
			// class-evaluation effects are accounted for at their execution sites.
			// A class declaration also supplies the constructor's concrete
			// `prototype` data property.
		case ast.KindPropertySignature, ast.KindMethodSignature, ast.KindIndexSignature:
			return c.unknownType
		default:
			return c.unknownType
		}
	}
	if writes && hasGetter && !hasSetter {
		// In strict JavaScript, assigning to a getter-only accessor throws a
		// TypeError. TypeScript normally permits readonly-to-mutable structural
		// assignment, so absence of a setter must not become a false `never`
		// effect through that alias.
		return c.unknownType
	}
	return c.unionThrowsTypes(effects...)
}

func (c *Checker) getPropertyReadThrowsType(property *ast.Symbol) *Type {
	return c.getPropertySymbolAccessThrowsType(property, true, false)
}

func (c *Checker) getPropertyWriteThrowsType(property *ast.Symbol) *Type {
	return c.getPropertySymbolAccessThrowsType(property, false, true)
}

func (c *Checker) getLocalClassConstructionThrowsType(node *ast.Node) (*Type, bool) {
	targetType := c.getTypeOfExpression(node.Expression())
	if targetType == nil || targetType.symbol == nil {
		return nil, false
	}
	declaration := ast.GetClassLikeDeclarationOfSymbol(c.getMergedSymbol(targetType.symbol))
	if declaration == nil || ast.GetSourceFileOfNode(declaration).IsDeclarationFile {
		return nil, false
	}
	return c.getClassConstructionThrowsType(declaration, node.Arguments(), make(map[*ast.Node]bool)), true
}

func (c *Checker) getClassConstructionThrowsType(classDeclaration *ast.Node, arguments []*ast.Node, seen map[*ast.Node]bool) *Type {
	if classDeclaration == nil || seen[classDeclaration] {
		return c.unknownType
	}
	seen[classDeclaration] = true
	defer delete(seen, classDeclaration)

	var effects []*Type
	for _, member := range classDeclaration.Members() {
		if member.Kind == ast.KindPropertyDeclaration && !ast.HasStaticModifier(member) && member.Initializer() != nil {
			effects = append(effects, c.collectRaisedTypes(member.Initializer())...)
		}
	}
	if constructor := ast.FindConstructorDeclaration(classDeclaration); constructor != nil {
		effects = append(effects, c.getThrowsTypeOfSignature(c.getSignatureFromDeclaration(constructor)))
		return c.unionThrowsTypes(effects...)
	}

	base := ast.GetExtendsHeritageClauseElement(classDeclaration)
	if base == nil {
		return c.unionThrowsTypes(effects...)
	}
	baseExpression := base.Expression()
	if c.isSafeGlobalErrorConstruction(baseExpression, arguments) {
		return c.unionThrowsTypes(effects...)
	}
	baseType := c.getTypeOfExpression(baseExpression)
	if baseType != nil && baseType.symbol != nil {
		baseDeclaration := ast.GetClassLikeDeclarationOfSymbol(c.getMergedSymbol(baseType.symbol))
		if baseDeclaration != nil && !ast.GetSourceFileOfNode(baseDeclaration).IsDeclarationFile {
			effects = append(effects, c.getClassConstructionThrowsType(baseDeclaration, arguments, seen))
			return c.unionThrowsTypes(effects...)
		}
	}
	return c.unknownType
}

func (c *Checker) isSafeGlobalErrorConstruction(expression *ast.Node, arguments []*ast.Node) bool {
	if expression == nil || expression.Kind != ast.KindIdentifier || expression.Text() != "Error" || len(arguments) > 1 {
		return false
	}
	symbol := c.getResolvedSymbol(expression)
	if symbol == nil || symbol == c.unknownSymbol || len(symbol.Declarations) == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		if !c.program.IsSourceFileDefaultLibrary(ast.GetSourceFileOfNode(declaration).Path()) {
			return false
		}
	}
	if len(arguments) == 0 {
		return true
	}
	argumentType := c.getTypeOfExpression(arguments[0])
	return argumentType != nil && argumentType.flags&(TypeFlagsStringLike|TypeFlagsUndefined) != 0
}

func isCallTargetPropertyAccess(node *ast.Node) bool {
	parent := node.Parent
	return parent != nil &&
		(parent.Kind == ast.KindCallExpression || parent.Kind == ast.KindNewExpression || parent.Kind == ast.KindTaggedTemplateExpression) &&
		parent.Expression() == node
}

func isAwaitedCall(node *ast.Node) bool {
	return node.Parent != nil && node.Parent.Kind == ast.KindAwaitExpression
}

func isDirectlyReturnedCall(node *ast.Node) bool {
	if node.Parent == nil {
		return false
	}
	return node.Parent.Kind == ast.KindReturnStatement && node.Parent.Expression() == node ||
		node.Parent.Kind == ast.KindArrowFunction && node.Parent.Body() == node
}

func (c *Checker) callReturnsPromise(node *ast.Node) bool {
	signature := c.getResolvedSignature(node, nil /*candidatesOutArray*/, CheckModeNormal)
	if signature == nil {
		return false
	}
	// Promise timing is a property of the declared return shape. Legacy `any`,
	// `unknown`, and broad object returns remain ordinary synchronous unknown
	// boundaries; treating every such call as a floating promise makes common
	// declaration APIs such as JSON.parse impossible to catch. Any is instead
	// quarantined when it crosses a checked capability or return boundary.
	return c.typeHasPromiseConstituent(c.getReturnTypeOfSignature(signature), make(map[*Type]bool))
}

func (c *Checker) typeHasPromiseConstituent(t *Type, seen map[*Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if t.flags&TypeFlagsUnionOrIntersection != 0 {
		for _, constituent := range t.Types() {
			if c.typeHasPromiseConstituent(constituent, seen) {
				return true
			}
		}
		return false
	}
	return c.getAwaitedTypeOfPromise(t) != nil
}

func (c *Checker) typeMayHideThenable(t *Type, seen map[*Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if t.flags&TypeFlagsUnionOrIntersection != 0 {
		for _, constituent := range t.Types() {
			if c.typeMayHideThenable(constituent, seen) {
				return true
			}
		}
		return false
	}
	return t.flags&(TypeFlagsAnyOrUnknown|TypeFlagsObject|TypeFlagsInstantiableNonPrimitive) != 0
}

func (c *Checker) functionAssimilatesThenables(decl *ast.Node) bool {
	if decl == nil {
		return false
	}
	if ast.GetFunctionFlags(decl)&ast.FunctionFlagsAsync != 0 {
		return true
	}
	returnTypeNode := decl.Type()
	return returnTypeNode != nil && c.typeHasPromiseConstituent(c.getTypeFromTypeNode(returnTypeNode), make(map[*Type]bool))
}

func (c *Checker) getReturnedPromiseThrowsType(expression *ast.Node, enclosingFn *ast.Node) *Type {
	if expression == nil {
		return nil
	}
	if expression.Kind == ast.KindCallExpression || expression.Kind == ast.KindNewExpression {
		if c.callReturnsPromise(expression) {
			return c.getThrowsTypeOfCall(expression)
		}
		if c.functionAssimilatesThenables(enclosingFn) && c.typeMayHideThenable(c.getTypeOfExpression(expression), make(map[*Type]bool)) {
			// An async function (or a function declared to return a Promise) adopts a
			// returned thenable. At this actual assimilation boundary, a legacy broad
			// value has an unknown rejection effect even though the producer call is
			// not itself classified as asynchronous.
			return c.unknownType
		}
		return nil
	}
	expressionType := c.getTypeOfExpression(expression)
	if c.typeHasPromiseConstituent(expressionType, make(map[*Type]bool)) ||
		c.functionAssimilatesThenables(enclosingFn) && c.typeMayHideThenable(expressionType, make(map[*Type]bool)) {
		// Promise values do not yet carry a rejection slot in their type. A
		// non-call expression therefore has no precise producer signature from
		// which to recover the rejection effect.
		return c.unknownType
	}
	return nil
}

func (c *Checker) collectReturnedPromiseEffects(decl *ast.Node) []*Type {
	if decl == nil || decl.Body() == nil {
		return nil
	}
	var effects []*Type
	add := func(expression *ast.Node) {
		if effect := c.getReturnedPromiseThrowsType(expression, decl); effect != nil && effect.flags&TypeFlagsNever == 0 {
			effects = append(effects, effect)
		}
	}
	body := decl.Body()
	if decl.Kind == ast.KindArrowFunction && body.Kind != ast.KindBlock {
		add(body)
		return effects
	}
	var visit func(*ast.Node) bool
	visit = func(node *ast.Node) bool {
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindClassDeclaration, ast.KindClassExpression:
			return false
		case ast.KindReturnStatement:
			add(node.Expression())
			return false
		}
		return node.ForEachChild(visit)
	}
	body.ForEachChild(visit)
	return effects
}

func (c *Checker) getThrowsTypeOfAwait(node *ast.Node) *Type {
	expression := node.Expression()
	if expression != nil && expression.Kind == ast.KindCallExpression {
		return c.getThrowsTypeOfCall(expression)
	}
	return c.unknownType
}

// getEscapingCapabilityThrowsType returns the effect of executable
// capabilities in arguments, including callbacks, constructors, accessors,
// and capabilities returned by them. Capabilities may be nested in option
// objects and collections.
// Without a call-site contract that proves synchronous invocation, a callee may
// retain and execute one after the caller's try/catch and throws boundary no
// longer exists. Fail-closed analysis therefore requires each exposed
// capability to handle its own effect until invocation-timing and ownership
// contracts are represented in signatures.
func (c *Checker) getEscapingCapabilityThrowsType(node *ast.Node) *Type {
	var capabilityEffects []*Type
	seen := make(map[*Type]bool)
	for _, argument := range node.Arguments() {
		c.collectEscapingCapabilityEffects(c.getTypeOfExpression(argument), seen, &capabilityEffects)
	}
	if len(capabilityEffects) == 0 {
		return nil
	}
	return c.getUnionType(capabilityEffects)
}

func (c *Checker) isAnyAliasPropertyMutation(node *ast.Node) bool {
	if node == nil || (node.Kind != ast.KindPropertyAccessExpression && node.Kind != ast.KindElementAccessExpression) {
		return false
	}
	baseType := c.getTypeOfExpression(node.Expression())
	return baseType != nil && baseType.flags&TypeFlagsAnyOrUnknown != 0 &&
		(getAssignmentTargetKind(node) != AssignmentKindNone || node.Parent != nil && node.Parent.Kind == ast.KindDeleteExpression)
}

func (c *Checker) collectEscapingCapabilityEffects(t *Type, seen map[*Type]bool, effects *[]*Type) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true

	// `any`, `unknown`, and unresolved type variables may hide a callable
	// capability. Treating them as inert would reopen the same boundary hole as
	// an unannotated ambient declaration.
	if t.flags&(TypeFlagsAnyOrUnknown|TypeFlagsInstantiableNonPrimitive) != 0 {
		*effects = append(*effects, c.unknownType)
		return
	}
	if t.flags&TypeFlagsUnionOrIntersection != 0 {
		for _, constituent := range t.Types() {
			c.collectEscapingCapabilityEffects(constituent, seen, effects)
		}
		return
	}

	for _, signature := range c.getSignaturesOfType(t, SignatureKindCall) {
		if effect := c.getThrowsTypeOfSignature(signature); effect != nil && effect.flags&TypeFlagsNever == 0 {
			*effects = append(*effects, c.normalizeThrowsType(effect))
		}
		c.collectEscapingCapabilityEffects(c.getReturnTypeOfSignature(signature), seen, effects)
	}
	constructSignatures := c.getSignaturesOfType(t, SignatureKindConstruct)
	for _, signature := range constructSignatures {
		if effect := c.getConstructCapabilityThrowsType(signature); effect != nil && effect.flags&TypeFlagsNever == 0 {
			*effects = append(*effects, c.normalizeThrowsType(effect))
		}
		c.collectEscapingCapabilityEffects(c.getReturnTypeOfSignature(signature), seen, effects)
	}
	if t.flags&TypeFlagsObject == 0 {
		return
	}
	for _, property := range c.getPropertiesOfType(t) {
		// A class constructor's synthetic `prototype` slot is a concrete data
		// property. Recurse into the instance shape, but do not mistake the
		// synthetic declaration metadata for an unknown accessor.
		if property.Name != "prototype" || len(constructSignatures) == 0 {
			if effect := c.getPropertyReadThrowsType(property); effect != nil && effect.flags&TypeFlagsNever == 0 {
				*effects = append(*effects, c.normalizeThrowsType(effect))
			}
			// A retained consumer cannot write through a readonly property in the
			// exposed type. Getter-only write rejection is still enforced when a
			// structural assignment tries to erase that readonly boundary.
			if !c.isReadonlySymbol(property) {
				if effect := c.getPropertyWriteThrowsType(property); effect != nil && effect.flags&TypeFlagsNever == 0 {
					*effects = append(*effects, c.normalizeThrowsType(effect))
				}
			}
		}
		c.collectEscapingCapabilityEffects(c.getTypeOfSymbol(property), seen, effects)
	}
	for _, indexInfo := range c.getIndexInfosOfType(t) {
		c.collectEscapingCapabilityEffects(indexInfo.valueType, seen, effects)
	}
}

func (c *Checker) getConstructCapabilityThrowsType(signature *Signature) *Type {
	if signature != nil && signature.declaration != nil {
		declaration := signature.declaration
		if ast.IsClassLike(declaration) && !ast.GetSourceFileOfNode(declaration).IsDeclarationFile {
			return c.getClassConstructionThrowsType(declaration, nil, make(map[*ast.Node]bool))
		}
	}
	return c.getThrowsTypeOfSignature(signature)
}

func (c *Checker) getCombinedCallThrowsType(t *Type, unknownWhenNotCallable bool) *Type {
	return c.getCombinedSignatureThrowsType(t, SignatureKindCall, unknownWhenNotCallable)
}

func (c *Checker) getCombinedConstructThrowsType(t *Type, unknownWhenNotConstructable bool) *Type {
	return c.getCombinedSignatureThrowsType(t, SignatureKindConstruct, unknownWhenNotConstructable)
}

func (c *Checker) getCombinedSignatureThrowsType(t *Type, kind SignatureKind, unknownWhenMissing bool) *Type {
	signatures := c.getSignaturesOfType(t, kind)
	if len(signatures) == 0 {
		if unknownWhenMissing {
			return c.unknownType
		}
		return nil
	}
	var effects []*Type
	for _, signature := range signatures {
		var effect *Type
		if kind == SignatureKindConstruct {
			effect = c.getConstructCapabilityThrowsType(signature)
		} else {
			effect = c.getThrowsTypeOfSignature(signature)
		}
		if effect != nil {
			effects = append(effects, c.normalizeThrowsType(effect))
		}
	}
	if len(effects) == 0 {
		return c.neverType
	}
	return c.getUnionType(effects)
}

// typeRequiresCheckedEffectProof reports whether assigning an `any` value to
// t would manufacture a checked-effect guarantee. Ordinary TypeScript still
// permits `any` at data-only boundaries, but it cannot silently become a
// callable, constructable, accessor, or concrete-property value whose effects
// the checker will later treat as known.
func (c *Checker) typeRequiresCheckedEffectProof(t *Type, seen map[*Type]bool) bool {
	if t == nil || seen[t] || t.flags&TypeFlagsAnyOrUnknown != 0 {
		return false
	}
	seen[t] = true
	if t.flags&TypeFlagsInstantiableNonPrimitive != 0 {
		// An unresolved type parameter can later instantiate to a callable,
		// constructor, accessor-bearing object, or concrete data-property type.
		// Allowing `any` to flow into it would defer effect laundering until the
		// instantiation site.
		return true
	}
	if t.flags&TypeFlagsUnionOrIntersection != 0 {
		for _, constituent := range t.Types() {
			if c.typeRequiresCheckedEffectProof(constituent, seen) {
				return true
			}
		}
		return false
	}

	for _, kind := range []SignatureKind{SignatureKindCall, SignatureKindConstruct} {
		for _, signature := range c.getSignaturesOfType(t, kind) {
			effect := c.getThrowsTypeOfSignature(signature)
			if effect == nil || effect.flags&TypeFlagsAnyOrUnknown == 0 {
				return true
			}
			if c.typeRequiresCheckedEffectProof(c.getReturnTypeOfSignature(signature), seen) {
				return true
			}
		}
	}
	if t.flags&TypeFlagsObject == 0 {
		return false
	}
	for _, property := range c.getPropertiesOfType(t) {
		readEffect := c.getPropertyReadThrowsType(property)
		if readEffect == nil || readEffect.flags&TypeFlagsAnyOrUnknown == 0 {
			return true
		}
		if !c.isReadonlySymbol(property) {
			writeEffect := c.getPropertyWriteThrowsType(property)
			if writeEffect == nil || writeEffect.flags&TypeFlagsAnyOrUnknown == 0 {
				return true
			}
		}
		if c.typeRequiresCheckedEffectProof(c.getTypeOfSymbol(property), seen) {
			return true
		}
	}
	for _, indexInfo := range c.getIndexInfosOfType(t) {
		if c.typeRequiresCheckedEffectProof(indexInfo.valueType, seen) {
			return true
		}
	}
	return false
}

// checkFailClosedAssertionEffects prevents a type assertion from erasing the
// effect of a callable value. Assertions may change value shapes, but
// `unknown as (() => void throws never)` cannot manufacture a proof that
// invoking unknown code is non-throwing.
func (c *Checker) checkFailClosedAssertionEffects(node *ast.Node) {
	targetType := c.getTypeFromTypeNode(node.Type())
	sourceType := c.getTypeOfExpression(node.Expression())
	c.checkAssertionPropertyEffectNarrowing(node, sourceType, targetType, make(map[[2]*Type]bool))
}

func (c *Checker) checkAssertionPropertyEffectNarrowing(node *ast.Node, source *Type, target *Type, seen map[[2]*Type]bool) {
	if source == nil || target == nil || target.flags&TypeFlagsStructuredOrInstantiable == 0 {
		return
	}
	pair := [2]*Type{source, target}
	if seen[pair] {
		return
	}
	seen[pair] = true
	if target.flags&TypeFlagsInstantiableNonPrimitive != 0 && source.flags&TypeFlagsAnyOrUnknown != 0 {
		c.errorCheckedExceptions(node, diagnostics.An_assertion_from_0_to_the_unresolved_type_1_can_manufacture_checked_effect_guarantees_after_instantiation, c.TypeToString(source), c.TypeToString(target))
		return
	}
	for _, effects := range [][2]*Type{
		{c.getCombinedCallThrowsType(source, true), c.getCombinedCallThrowsType(target, false)},
		{c.getCombinedConstructThrowsType(source, true), c.getCombinedConstructThrowsType(target, false)},
	} {
		sourceEffect, targetEffect := effects[0], effects[1]
		if targetEffect != nil && !c.isTypeAssignableTo(sourceEffect, targetEffect) {
			c.errorCheckedExceptions(node, diagnostics.The_source_signature_may_throw_0_but_the_target_s_throws_clause_only_permits_1, c.TypeToString(sourceEffect), c.TypeToString(targetEffect))
			return
		}
	}

	for _, targetProperty := range c.getPropertiesOfType(target) {
		sourceProperty := c.getPropertyOfType(source, targetProperty.Name)
		var sourceRead, sourceWrite *Type
		if source.flags&TypeFlagsAnyOrUnknown != 0 || sourceProperty == nil {
			sourceRead, sourceWrite = c.unknownType, c.unknownType
		} else {
			sourceRead = c.getPropertyReadThrowsType(sourceProperty)
			sourceWrite = c.getPropertyWriteThrowsType(sourceProperty)
		}
		targetRead := c.getPropertyReadThrowsType(targetProperty)
		if sourceRead == nil {
			sourceRead = c.neverType
		}
		if targetRead == nil {
			targetRead = c.neverType
		}
		if !c.isTypeAssignableTo(sourceRead, targetRead) {
			c.errorCheckedExceptions(node, diagnostics.Property_0_s_1_effect_may_throw_2_but_the_target_only_permits_3,
				targetProperty.Name, "read", c.TypeToString(sourceRead), c.TypeToString(targetRead))
			continue
		}
		if !c.isReadonlySymbol(targetProperty) {
			targetWrite := c.getPropertyWriteThrowsType(targetProperty)
			if sourceWrite == nil {
				sourceWrite = c.neverType
			}
			if targetWrite == nil {
				targetWrite = c.neverType
			}
			if !c.isTypeAssignableTo(sourceWrite, targetWrite) {
				c.errorCheckedExceptions(node, diagnostics.Property_0_s_1_effect_may_throw_2_but_the_target_only_permits_3,
					targetProperty.Name, "write", c.TypeToString(sourceWrite), c.TypeToString(targetWrite))
				continue
			}
		}
		sourcePropertyType := c.unknownType
		if sourceProperty != nil {
			sourcePropertyType = c.getTypeOfSymbol(sourceProperty)
		}
		c.checkAssertionPropertyEffectNarrowing(node, sourcePropertyType, c.getTypeOfSymbol(targetProperty), seen)
	}
}

func (c *Checker) checkFailClosedOverloadEffects(implementation *ast.Node) {
	if implementation.Body() == nil {
		return
	}
	symbol := c.getSymbolOfDeclaration(implementation)
	if symbol == nil {
		return
	}
	implementationEffect := c.getThrowsTypeOfSignature(c.getSignatureFromDeclaration(implementation))
	if implementationEffect == nil {
		implementationEffect = c.neverType
	}
	implementationEffect = c.normalizeThrowsType(implementationEffect)
	for _, overload := range c.getSignaturesOfSymbol(symbol) {
		if overload.declaration == nil || overload.declaration == implementation {
			continue
		}
		overloadEffect := c.getThrowsTypeOfSignature(overload)
		if overloadEffect == nil {
			overloadEffect = c.neverType
		}
		overloadEffect = c.normalizeThrowsType(overloadEffect)
		if !c.isTypeAssignableTo(implementationEffect, overloadEffect) {
			c.errorCheckedExceptions(overload.declaration, diagnostics.The_source_signature_may_throw_0_but_the_target_s_throws_clause_only_permits_1, c.TypeToString(implementationEffect), c.TypeToString(overloadEffect))
		}
	}
}

// isFailClosedImplicitUnknownOperation identifies ECMAScript operations whose
// abrupt-completion behavior is not represented by a resolved call signature.
// Checked-exceptions analysis fails closed on these operations until a precise effect is
// available. The deliberately-safe binary operators below do not perform user
// coercion or invoke custom matchers.
func (c *Checker) isFailClosedImplicitUnknownOperation(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindTaggedTemplateExpression,
		ast.KindDeleteExpression,
		ast.KindPostfixUnaryExpression,
		ast.KindTemplateExpression,
		ast.KindYieldExpression,
		ast.KindSpreadElement,
		ast.KindSpreadAssignment,
		ast.KindForInStatement,
		ast.KindForOfStatement,
		ast.KindWithStatement,
		ast.KindObjectBindingPattern,
		ast.KindArrayBindingPattern,
		ast.KindComputedPropertyName,
		ast.KindDecorator,
		ast.KindImportDeclaration,
		ast.KindImportEqualsDeclaration,
		ast.KindExportDeclaration,
		ast.KindJsxElement,
		ast.KindJsxSelfClosingElement,
		ast.KindJsxFragment,
		ast.KindJsxSpreadAttribute:
		return true
	case ast.KindVariableDeclarationList:
		// `using` and `await using` execute user-defined disposal hooks when the
		// scope exits, which may be later than the declaration's initializer.
		return node.Flags&ast.NodeFlagsUsing != 0
	case ast.KindPrefixUnaryExpression:
		// Logical negation performs ToBoolean, which cannot invoke user code.
		operator := node.AsPrefixUnaryExpression().Operator
		if operator == ast.KindExclamationToken {
			return false
		}
		operandType := c.getTypeOfExpression(node.AsPrefixUnaryExpression().Operand)
		if operandType == nil {
			return true
		}
		switch operator {
		case ast.KindPlusToken:
			// Unary plus rejects BigInt at runtime.
			return operandType.flags&TypeFlagsNumberLike == 0
		case ast.KindMinusToken, ast.KindTildeToken:
			return operandType.flags&(TypeFlagsNumberLike|TypeFlagsBigIntLike) == 0
		default:
			return true
		}
	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		switch binary.OperatorToken.Kind {
		case ast.KindEqualsEqualsEqualsToken,
			ast.KindExclamationEqualsEqualsToken,
			ast.KindAmpersandAmpersandToken,
			ast.KindBarBarToken,
			ast.KindQuestionQuestionToken,
			ast.KindCommaToken:
			return false
		case ast.KindEqualsToken:
			// Object and array assignment patterns execute property/iterator
			// protocols even though a simple lexical assignment does not.
			return binary.Left.Kind == ast.KindObjectLiteralExpression || binary.Left.Kind == ast.KindArrayLiteralExpression
		case ast.KindPlusToken:
			left := c.getTypeOfExpression(binary.Left)
			right := c.getTypeOfExpression(binary.Right)
			if left == nil || right == nil {
				return true
			}
			return !((left.flags&TypeFlagsStringLike != 0 && right.flags&TypeFlagsStringLike != 0) ||
				(left.flags&TypeFlagsNumberLike != 0 && right.flags&TypeFlagsNumberLike != 0) ||
				(left.flags&TypeFlagsBigIntLike != 0 && right.flags&TypeFlagsBigIntLike != 0))
		case ast.KindMinusToken, ast.KindAsteriskToken:
			left := c.getTypeOfExpression(binary.Left)
			right := c.getTypeOfExpression(binary.Right)
			return left == nil || right == nil ||
				!((left.flags&TypeFlagsNumberLike != 0 && right.flags&TypeFlagsNumberLike != 0) ||
					(left.flags&TypeFlagsBigIntLike != 0 && right.flags&TypeFlagsBigIntLike != 0))
		default:
			return true
		}
	}
	return false
}

// getClassEvaluationThrowsType accounts only for work performed while the
// class definition itself is evaluated. Method bodies and instance field
// initializers run later and must not poison an otherwise inert declaration.
func (c *Checker) getClassEvaluationThrowsType(node *ast.Node) *Type {
	var effects []*Type
	if ast.HasDecorators(node) {
		effects = append(effects, c.unknownType)
	}
	if base := ast.GetExtendsHeritageClauseElement(node); base != nil {
		baseExpression := base.Expression()
		effects = append(effects, c.collectRaisedTypes(baseExpression)...)
		baseType := c.getTypeOfExpression(baseExpression)
		if baseType == nil || baseType.flags&TypeFlagsAnyOrUnknown != 0 {
			// Evaluating `extends value` performs IsConstructor and throws when a
			// dynamic value is not constructable. A statically `any` base is not a
			// proof that class evaluation can complete normally.
			effects = append(effects, c.unknownType)
		}
	}
	for _, member := range node.Members() {
		if ast.HasDecorators(member) || member.Name() != nil && member.Name().Kind == ast.KindComputedPropertyName {
			effects = append(effects, c.unknownType)
		}
		switch member.Kind {
		case ast.KindClassStaticBlockDeclaration:
			effects = append(effects, c.collectRaisedTypes(member.Body())...)
		case ast.KindPropertyDeclaration:
			if ast.HasStaticModifier(member) && member.Initializer() != nil {
				effects = append(effects, c.collectRaisedTypes(member.Initializer())...)
			}
		}
	}
	return c.unionThrowsTypes(effects...)
}

// collectRaisedTypes gathers the types a statement subtree can raise past any
// try...catch statements it contains. Nested function bodies do not raise
// (calling them does); a try block whose statement has a catch clause is fully
// discharged, though its catch and finally blocks still raise outward.
func (c *Checker) collectRaisedTypes(node *ast.Node) []*Type {
	return c.collectRaisedTypesWorker(node, false /*catchableOnly*/)
}

// collectCatchableRaisedTypes is narrower than function-effect inference: a
// synchronous catch can observe immediate abrupt completion and awaited
// rejection, but not a rejection from a floating or directly returned promise.
func (c *Checker) collectCatchableRaisedTypes(node *ast.Node) []*Type {
	return c.collectRaisedTypesWorker(node, true /*catchableOnly*/)
}

func (c *Checker) collectRaisedTypesWorker(node *ast.Node, catchableOnly bool) []*Type {
	var raised []*Type
	var visit func(node *ast.Node) bool
	add := func(t *Type) {
		if t != nil && t != c.errorType && t.flags&TypeFlagsNever == 0 {
			raised = append(raised, c.normalizeThrowsType(t))
		}
	}
	visit = func(node *ast.Node) bool {
		if c.checkedExceptionsFailClosed() && c.isFailClosedImplicitUnknownOperation(node) {
			add(c.unknownType)
		}
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindClassStaticBlockDeclaration:
			return false
		case ast.KindClassDeclaration, ast.KindClassExpression:
			add(c.getClassEvaluationThrowsType(node))
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
			promiseEscapesCatch := catchableOnly && c.checkedExceptionsFailClosed() && c.callReturnsPromise(node) && !isAwaitedCall(node)
			if c.checkedExceptionsFailClosed() && !promiseEscapesCatch {
				add(c.getEscapingCapabilityThrowsType(node))
			}
			if !promiseEscapesCatch {
				add(c.getThrowsTypeOfCall(node))
			}
		case ast.KindAwaitExpression:
			if c.checkedExceptionsFailClosed() {
				add(c.getThrowsTypeOfAwait(node))
			}
		case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
			if c.checkedExceptionsFailClosed() && !isCallTargetPropertyAccess(node) {
				add(c.getPropertyAccessThrowsType(node))
			}
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
	if c.catchClauseThrowsInProgress == nil {
		c.catchClauseThrowsInProgress = make(map[*ast.Node]bool)
	}
	if c.catchClauseThrowsInProgress[catchClause] {
		// A cyclic catch-type query must not erase an effect. Enabled analysis
		// uses the top effect; disabled analysis retains existing behavior.
		if c.checkedExceptionsFailClosed() {
			return c.unknownType
		}
		return nil
	}
	if cached, ok := c.catchClauseThrowsTypes[catchClause]; ok {
		return cached
	}
	c.catchClauseThrowsInProgress[catchClause] = true
	defer delete(c.catchClauseThrowsInProgress, catchClause)
	var result *Type
	if tryStatement := catchClause.Parent; tryStatement != nil && tryStatement.Kind == ast.KindTryStatement {
		raised := c.collectCatchableRaisedTypes(tryStatement.AsTryStatement().TryBlock)
		if len(raised) != 0 {
			result = c.getUnionType(raised)
		}
	}
	if c.throwsInference != nil {
		// Computed from provisional inference results: usable now, but not safe
		// to cache past the enclosing fixpoint iteration.
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
		if c.checkedExceptionsFailClosed() && tryDepth == 0 && c.isFailClosedImplicitUnknownOperation(node) {
			c.checkRaisedType(node, c.unknownType, enclosingFn, false /*isCall*/)
		}
		switch node.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
			ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
			ast.KindClassStaticBlockDeclaration:
			if c.checkedExceptionsFailClosed() && (node.Kind == ast.KindFunctionDeclaration || node.Kind == ast.KindMethodDeclaration || node.Kind == ast.KindConstructor) {
				c.checkFailClosedOverloadEffects(node)
			}
			savedFn, savedTryDepth := enclosingFn, tryDepth
			enclosingFn, tryDepth = node, 0
			if node.Kind == ast.KindArrowFunction && node.Body() != nil && node.Body().Kind != ast.KindBlock &&
				node.Body().Kind != ast.KindCallExpression && node.Body().Kind != ast.KindNewExpression {
				c.checkRaisedType(node.Body(), c.getReturnedPromiseThrowsType(node.Body(), node), node, false /*isCall*/)
			}
			node.ForEachChild(visit)
			enclosingFn, tryDepth = savedFn, savedTryDepth
			return false
		case ast.KindClassDeclaration, ast.KindClassExpression:
			if c.checkedExceptionsFailClosed() && tryDepth == 0 {
				c.checkRaisedType(node, c.getClassEvaluationThrowsType(node), enclosingFn, false /*isCall*/)
			}
			// Class evaluation belongs to the surrounding boundary, but member
			// bodies still need independent validation against their own explicit
			// throws clauses even when no call or construction site exists. Treat
			// the class as the propagation boundary for field initializers; their
			// effects are incorporated into construction separately.
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
			if c.checkedExceptionsFailClosed() {
				if c.isUnsupportedProxyFactoryCall(node) {
					c.errorCheckedExceptions(node, diagnostics.Proxy_values_have_latent_property_and_private_brand_effects_that_are_not_representable_by_checked_exceptions_Proxy_construction_and_revocation_are_not_supported_when_checkedExceptions_is_enabled)
				}
				if capabilityEffect := c.getEscapingCapabilityThrowsType(node); capabilityEffect != nil {
					c.errorCheckedExceptions(node, diagnostics.Argument_exposes_executable_code_that_may_throw_0_after_the_call_returns_A_synchronous_try_catch_or_enclosing_throws_clause_cannot_handle_it_Handle_the_effect_inside_the_capability_or_use_a_non_escaping_contract, c.TypeToString(capabilityEffect))
				}
			}
			returnsPromise := c.checkedExceptionsFailClosed() && c.callReturnsPromise(node)
			if returnsPromise && !isAwaitedCall(node) && !isDirectlyReturnedCall(node) {
				throwsType := c.normalizeThrowsType(c.getThrowsTypeOfCall(node))
				if throwsType == nil || throwsType.flags&TypeFlagsNever != 0 {
					throwsType = c.unknownType
				}
				c.errorCheckedExceptions(node, diagnostics.Promise_returning_call_may_reject_with_0_A_synchronous_try_catch_does_not_handle_that_rejection_Await_or_return_the_promise, c.TypeToString(throwsType))
			} else if returnsPromise && isDirectlyReturnedCall(node) {
				// Returning a promise propagates its rejection effect even from inside
				// a synchronous try block. Only `await` transfers a rejection into the
				// surrounding catchable control-flow position.
				c.checkRaisedType(node, c.getThrowsTypeOfCall(node), enclosingFn, true /*isCall*/)
			} else if c.checkedExceptionsFailClosed() && isDirectlyReturnedCall(node) {
				// Broad legacy values are only considered possibly thenable at an
				// enclosing Promise-assimilation boundary, not at every call site.
				returnedEffect := c.getReturnedPromiseThrowsType(node, enclosingFn)
				if returnedEffect != nil {
					c.checkRaisedType(node, returnedEffect, enclosingFn, true /*isCall*/)
				} else if tryDepth == 0 {
					c.checkRaisedType(node, c.getThrowsTypeOfCall(node), enclosingFn, true /*isCall*/)
				}
			} else if tryDepth == 0 && !(c.checkedExceptionsFailClosed() && isAwaitedCall(node)) {
				c.checkRaisedType(node, c.getThrowsTypeOfCall(node), enclosingFn, true /*isCall*/)
			}
		case ast.KindAwaitExpression:
			if c.checkedExceptionsFailClosed() && tryDepth == 0 {
				c.checkRaisedType(node, c.getThrowsTypeOfAwait(node), enclosingFn, false /*isCall*/)
			}
		case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
			if c.checkedExceptionsFailClosed() && c.isAnyAliasPropertyMutation(node) {
				c.errorCheckedExceptions(node, diagnostics.Mutating_a_property_through_an_any_or_unknown_alias_can_corrupt_a_checked_effect_contract)
			}
			if c.checkedExceptionsFailClosed() && tryDepth == 0 && !isCallTargetPropertyAccess(node) {
				c.checkRaisedType(node, c.getPropertyAccessThrowsType(node), enclosingFn, false /*isCall*/)
			}
		case ast.KindTypeAssertionExpression, ast.KindAsExpression:
			if c.checkedExceptionsFailClosed() {
				c.checkFailClosedAssertionEffects(node)
			}
		case ast.KindReturnStatement:
			expression := node.Expression()
			if c.checkedExceptionsFailClosed() && expression != nil && expression.Kind != ast.KindCallExpression && expression.Kind != ast.KindNewExpression {
				// A synchronous try cannot discharge a rejection carried by the
				// returned promise value, so this deliberately ignores tryDepth.
				c.checkRaisedType(node, c.getReturnedPromiseThrowsType(expression, enclosingFn), enclosingFn, false /*isCall*/)
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
	raised = c.normalizeThrowsType(raised)
	if raised == nil || raised == c.errorType || raised.flags&TypeFlagsNever != 0 {
		// Untracked or empty.
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
