package cel

import (
	"fmt"
	"maps"
	"regexp"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

const (
	StartToken = "${"
	EndToken   = "}"
)

var (
	celExpressionRe        = regexp.MustCompile(`\$\{([^}]+)\}`)
	resourcesExpressionRe  = regexp.MustCompile(`(resources\.[^.]+)\.`)
	referencesExpressionRe = regexp.MustCompile(`(refs\.[^.]+)\.`)
)

func SearchExpressions(expression string) []string {
	matches := celExpressionRe.FindAllStringSubmatch(expression, -1)

	celExpressions := make([]string, 0)
	for _, m := range matches {
		celExpressions = append(celExpressions, m[1])
	}

	return celExpressions
}

type CelExpression string

func NewCelExpression(source string) (CelExpression, error) {
	matches := celExpressionRe.FindStringSubmatch(source)

	if len(matches) == 0 {
		return CelExpression(""), fmt.Errorf("invalid cel expression: %s", source)
	}

	expression := matches[1]

	return CelExpression(expression), nil
}

func (e CelExpression) Source() string {
	return string(e)
}

func (e CelExpression) Dependencies() []string {
	dependencies := make([]string, 0)

	matches := resourcesExpressionRe.FindStringSubmatch(e.Source())
	if len(matches) != 0 {
		dependencies = append(dependencies, matches[1])
	}

	matches = referencesExpressionRe.FindStringSubmatch(e.Source())
	if len(matches) != 0 {
		dependencies = append(dependencies, matches[1])
	}

	return dependencies
}

func (e CelExpression) Evaluate(variables map[string]any) (string, error) {
	celEnvironmentOpts := make([]cel.EnvOption, 0)
	celEnvironmentOpts = append(celEnvironmentOpts,
		ext.Lists(),
		ext.Strings(),
	)
	for k := range maps.Keys(variables) {
		celEnvironmentOpts = append(celEnvironmentOpts, cel.Variable(k, cel.AnyType))
	}
	environment, err := cel.NewEnv(celEnvironmentOpts...)
	if err != nil {
		return "", err
	}

	source := e.Source()

	checkedAst, issues := environment.Compile(source)
	if issues != nil && issues.Err() != nil {
		return "", fmt.Errorf("failed compiling expression %s: %w", source, issues.Err())
	}

	program, err := environment.Program(checkedAst)
	if err != nil {
		return "", fmt.Errorf("failed programming expression %s: %w", source, err)
	}

	value, _, err := program.Eval(variables)
	if err != nil {
		return "", fmt.Errorf("failed evaluating expression %s: %w", source, err)
	}

	return value.Value().(string), nil
}
