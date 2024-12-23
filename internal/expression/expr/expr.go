package expr

import (
	"fmt"
	"maps"
	"regexp"

	"github.com/expr-lang/expr"
)

var (
	exprExpressionRe       = regexp.MustCompile(`\$\{([^}]+)\}`)
	resourcesExpressionRe  = regexp.MustCompile(`(resources\.[^.]+)\.`)
	referencesExpressionRe = regexp.MustCompile(`(refs\.[^.]+)\.`)
)

func SearchExpressions(expression string) []string {
	matches := exprExpressionRe.FindAllStringSubmatch(expression, -1)

	expressions := make([]string, 0)
	for _, m := range matches {
		expressions = append(expressions, m[1])
	}

	return expressions
}

type ExprExpression string

func NewExprExpression(source string) (ExprExpression, error) {
	matches := exprExpressionRe.FindStringSubmatch(source)

	if len(matches) == 0 {
		return ExprExpression(""), fmt.Errorf("invalid Expr expression: %s", source)
	}

	expression := matches[1]

	return ExprExpression(expression), nil
}

func (e ExprExpression) Source() string {
	return string(e)
}

func (e ExprExpression) Dependencies() []string {
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

func (e ExprExpression) Evaluate(args ...map[string]any) (any, error) {
	allArgs := make(map[string]any)
	for _, arg := range args {
		maps.Copy(allArgs, arg)
	}

	source := e.Source()

	program, err := expr.Compile(source, expr.Env(allArgs))
	if err != nil {
		return "", fmt.Errorf("failed compiling expression %s: %w", source, err)
	}

	value, err := expr.Run(program, allArgs)
	if err != nil {
		return "", fmt.Errorf("failed evaluating expression %s: %w", source, err)
	}

	return value, nil
}
