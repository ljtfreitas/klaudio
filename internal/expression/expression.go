package expression

import (
	"fmt"
	"strings"

	"github.com/nubank/klaudio/internal/expression/cel"
)

type Expression interface {
	Source() string
	Evaluate(map[string]any) (string, error)
	Dependencies() []string
}

func Parse(expression any) (Expression, error) {
	expressionAsString, ok := expression.(string)
	if !ok {
		return SimpleExpression(fmt.Sprintf("%s", expression)), nil
	}

	celExpressions := cel.SearchExpressions(expressionAsString)

	if len(celExpressions) == 0 {
		return SimpleExpression(expressionAsString), nil
	}

	if len(celExpressions) == 1 && strings.HasPrefix(expressionAsString, cel.StartToken) {
		return cel.NewCelExpression(expressionAsString)
	}

	return newCompositeExpression(expressionAsString, celExpressions)

}

func NoArgs() map[string]any {
	return make(map[string]any)
}

func noDependencies() []string {
	return make([]string, 0)
}

type SimpleExpression string

func (e SimpleExpression) Source() string {
	return string(e)
}

func (e SimpleExpression) Evaluate(map[string]any) (string, error) {
	return e.Source(), nil
}

func (e SimpleExpression) Dependencies() []string {
	return noDependencies()
}

type CompositeExpression struct {
	source         string
	celExpressions []cel.CelExpression
}

func newCompositeExpression(expression string, celExpressions []string) (CompositeExpression, error) {
	expressions := make([]cel.CelExpression, 0)
	for _, celExpression := range celExpressions {
		expressions = append(expressions, cel.CelExpression(celExpression))
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
		fragment := cel.StartToken + celExpression.Source() + cel.EndToken
		s = strings.Replace(s, fragment, r, -1)
	}
	return s, nil
}

func (e CompositeExpression) Dependencies() []string {
	dependencies := make([]string, len(e.celExpressions))
	for _, celExpression := range e.celExpressions {
		dependencies = append(dependencies, celExpression.Dependencies()...)
	}
	return dependencies
}
