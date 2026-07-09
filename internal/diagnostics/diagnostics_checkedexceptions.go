package diagnostics

// Messages for the checkedExceptions feature.
//
// These mirror the entries in extraDiagnosticMessages.json and use the exact
// names and keys the generator would produce. The generator (generate.go)
// requires the TypeScript submodule, which is not always available; when
// diagnostics_generated.go is next regenerated with these entries present in
// extraDiagnosticMessages.json, this file must be deleted.

var Enable_checked_exceptions_Functions_must_handle_the_errors_their_callees_declare_with_throws_clauses_or_declare_them_in_their_own = &Message{code: 100020, category: CategoryMessage, key: "Enable_checked_exceptions_Functions_must_handle_the_errors_their_callees_declare_with_throws_clauses_100020", text: "Enable checked exceptions. Functions must handle the errors their callees declare with 'throws' clauses, or declare them in their own."}

var Call_may_throw_0_which_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_function_s_throws_clause = &Message{code: 100021, category: CategoryError, key: "Call_may_throw_0_which_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_100021", text: "Call may throw '{0}', which is neither caught by an enclosing 'try' statement nor declared in the enclosing function's 'throws' clause."}

var Thrown_value_of_type_0_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_function_s_throws_clause = &Message{code: 100022, category: CategoryError, key: "Thrown_value_of_type_0_is_neither_caught_by_an_enclosing_try_statement_nor_declared_in_the_enclosing_100022", text: "Thrown value of type '{0}' is neither caught by an enclosing 'try' statement nor declared in the enclosing function's 'throws' clause."}

var The_source_signature_may_throw_0_but_the_target_s_throws_clause_only_permits_1 = &Message{code: 100023, category: CategoryError, key: "The_source_signature_may_throw_0_but_the_target_s_throws_clause_only_permits_1_100023", text: "The source signature may throw '{0}', but the target's 'throws' clause only permits '{1}'."}

var Call_may_throw_0_which_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_handle_all_checked_exceptions = &Message{code: 100024, category: CategoryError, key: "Call_may_throw_0_which_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_h_100024", text: "Call may throw '{0}', which is not caught by an enclosing 'try' statement. Code outside of a function must handle all checked exceptions."}

var Thrown_value_of_type_0_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_handle_all_checked_exceptions = &Message{code: 100025, category: CategoryError, key: "Thrown_value_of_type_0_is_not_caught_by_an_enclosing_try_statement_Code_outside_of_a_function_must_h_100025", text: "Thrown value of type '{0}' is not caught by an enclosing 'try' statement. Code outside of a function must handle all checked exceptions."}
