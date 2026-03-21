package dsl

const dslVersionStr = "1.0"

func DSLVersion() string {
	return dslVersionStr
}

func Parse(source string) (*Policy, []DSLError) {
	lexer := NewLexer(source)
	tokens := lexer.Tokenize()
	parser := NewParserWithSource(tokens, source)
	return parser.Parse()
}

func CompileSource(source string) (*CompiledPolicy, []DSLError, error) {
	policy, errs := Parse(source)

	for _, e := range errs {
		if e.Severity == "error" {
			return nil, errs, nil
		}
	}

	compiler := NewCompiler()
	compiled, err := compiler.Compile(policy)
	if err != nil {
		return nil, errs, err
	}

	return compiled, errs, nil
}

func CompileAST(ast *Policy) (*CompiledPolicy, error) {
	compiler := NewCompiler()
	return compiler.Compile(ast)
}

func EvaluateSource(source string, ctx SessionContext) (*PolicyResult, error) {
	compiled, errs, err := CompileSource(source)
	if err != nil {
		return nil, err
	}

	for _, e := range errs {
		if e.Severity == "error" {
			return nil, &e
		}
	}

	evaluator := NewEvaluator()
	return evaluator.Evaluate(ctx, compiled)
}

func EvaluateCompiled(compiled *CompiledPolicy, ctx SessionContext) (*PolicyResult, error) {
	evaluator := NewEvaluator()
	return evaluator.Evaluate(ctx, compiled)
}

func Validate(source string) []DSLError {
	_, errs := Parse(source)
	return errs
}
