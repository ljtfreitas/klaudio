package cel

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

type Expression interface {
	Source() string
	Evaluate(map[string]any) (string, error)
}

const (
	startToken = "${"
	endToken   = "}"
)

var (
	celExpressionRe = regexp.MustCompile(`\$\{([^}]+)\}`)
)

func Parse(expression any) (Expression, error) {
	expressionAsString, ok := expression.(string)
	if !ok {
		return SimpleExpression(fmt.Sprintf("%s", expression)), nil
	}

	matches := celExpressionRe.FindAllStringSubmatch(expressionAsString, -1)

	if len(matches) == 0 {
		return SimpleExpression(expressionAsString), nil
	}

	celExpressions := make([]string, 0)
	for _, m := range matches {
		celExpressions = append(celExpressions, m[1])
	}

	if len(celExpressions) == 1 && strings.HasPrefix(expressionAsString, startToken) {
		return newCelExpression(expressionAsString)
	}

	return newCompositeExpression(expressionAsString, celExpressions)

}

func NoArgs() map[string]any {
	return make(map[string]any)
}

type SimpleExpression string

func (e SimpleExpression) Source() string {
	return string(e)
}

func (e SimpleExpression) Evaluate(map[string]any) (string, error) {
	return e.Source(), nil
}

type CompositeExpression struct {
	source         string
	celExpressions []CelExpression
}

func newCompositeExpression(expression string, celExpressions []string) (CompositeExpression, error) {
	expressions := make([]CelExpression, 0)
	for _, celExpression := range celExpressions {
		expressions = append(expressions, CelExpression(celExpression))
	}

	return CompositeExpression{source: expression, celExpressions: expressions}, nil
}

func (e CompositeExpression) Source() string {
	return e.source
}

func (e CompositeExpression) Evaluate(variables map[string]any) (string, error) {
	s := e.source
	for _, celExpression := range e.celExpressions {
		r, err := celExpression.Evaluate(variables)
		if err != nil {
			return "", err
		}
		fragment := startToken + celExpression.Source() + endToken
		s = strings.Replace(s, fragment, r, -1)
	}
	return s, nil
}

type CelExpression string

func newCelExpression(source string) (CelExpression, error) {
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
