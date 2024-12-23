package expression

import (
	"fmt"
	"strings"

	"github.com/nubank/klaudio/internal/expression/expr"
)

const (
	StartToken = "${"
	EndToken   = "}"
)

type Expression interface {
	Source() string
	Evaluate(args ...map[string]any) (any, error)
	Dependencies() []string
}

func Parse(expression any) (Expression, error) {
	expressionAsString, ok := expression.(string)
	if !ok {
		return SimpleExpression(fmt.Sprintf("%s", expression)), nil
	}

	expressions := expr.SearchExpressions(expressionAsString)

	if len(expressions) == 0 {
		return SimpleExpression(expressionAsString), nil
	}

	if len(expressions) == 1 && strings.HasPrefix(expressionAsString, StartToken) {
		return expr.NewExprExpression(expressionAsString)
	}

	return newCompositeExpression(expressionAsString, expressions)

}

func noDependencies() []string {
	return make([]string, 0)
}

type SimpleExpression string

func (e SimpleExpression) Source() string {
	return string(e)
}

func (e SimpleExpression) Evaluate(args ...map[string]any) (any, error) {
	return e.Source(), nil
}

func (e SimpleExpression) Dependencies() []string {
	return noDependencies()
}

type CompositeExpression struct {
	source      string
	expressions []Expression
}

func newCompositeExpression(expression string, expressions []string) (CompositeExpression, error) {
	checkedExpressions := make([]Expression, 0)
	for _, e := range expressions {
		checkedExpressions = append(checkedExpressions, expr.ExprExpression(e))
	}

	return CompositeExpression{source: expression, expressions: checkedExpressions}, nil
}

func (e CompositeExpression) Source() string {
	return e.source
}

func (e CompositeExpression) Evaluate(args ...map[string]any) (any, error) {
	s := e.source
	for _, expression := range e.expressions {
		r, err := expression.Evaluate(args...)
		if err != nil {
			return "", err
		}
		fragment := StartToken + expression.Source() + EndToken
		s = strings.Replace(s, fragment, fmt.Sprintf("%s", r), -1)
	}
	return s, nil
}

func (e CompositeExpression) Dependencies() []string {
	dependencies := make([]string, len(e.expressions))
	for _, expression := range e.expressions {
		dependencies = append(dependencies, expression.Dependencies()...)
	}
	return dependencies
}
