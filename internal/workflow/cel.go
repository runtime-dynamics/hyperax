package workflow

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

// CELEvaluator evaluates Common Expression Language (CEL) condition strings
// against a context map. It is used by the workflow executor to determine
// whether a step's condition allows execution.
type CELEvaluator struct{}

// NewCELEvaluator creates a new CELEvaluator instance.
func NewCELEvaluator() *CELEvaluator {
	return &CELEvaluator{}
}

// Evaluate parses and evaluates a CEL expression against the provided variables.
// The variables map keys become available as identifiers in the expression.
// Returns true if the expression evaluates to boolean true, false otherwise.
// An empty expression always returns true (unconditional step).
func (e *CELEvaluator) Evaluate(expression string, variables map[string]interface{}) (bool, error) {
	if expression == "" {
		return true, nil
	}

	// Build CEL variable declarations from the provided map.
	opts := []cel.EnvOption{
		cel.Variable("ctx", cel.MapType(cel.StringType, cel.DynType)),
	}

	env, err := cel.NewEnv(opts...)
	if err != nil {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: %w", err)
	}

	ast, issues := env.Parse(expression)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: %w", issues.Err())
	}

	// Type-check is optional for dynamic types but catches obvious errors.
	checkedAST, issues := env.Check(ast)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: %w", issues.Err())
	}

	prog, err := env.Program(checkedAST)
	if err != nil {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: %w", err)
	}

	// Wrap the variables into a "ctx" map so expressions use ctx.foo syntax.
	activation := map[string]interface{}{
		"ctx": variables,
	}

	out, _, err := prog.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: %w", err)
	}

	result, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("workflow.CELEvaluator.Evaluate: CEL expression did not return bool, got %T", out.Value())
	}

	return result, nil
}
