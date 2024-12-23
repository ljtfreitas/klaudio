package expr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type ObjectArg struct {
	Object string
}

func Test_ExprExpression(t *testing.T) {

	t.Run("We should be able to eval a constant expression", func(t *testing.T) {
		expression, err := NewExprExpression(`${"sample"}`)

		assert.NoError(t, err)
		assert.Equal(t, `"sample"`, expression.Source())

		r, err := expression.Evaluate(make(map[string]any))

		assert.NoError(t, err)
		assert.Equal(t, "sample", r)
	})

	t.Run("We should be able to eval an array expression", func(t *testing.T) {
		expression, err := NewExprExpression(`${sample[1]}`)

		assert.NoError(t, err)
		assert.Equal(t, "sample[1]", expression.Source())

		variables := map[string]any{
			"sample": []string{
				"hello",
				"world",
			},
		}

		r, err := expression.Evaluate(variables)

		assert.NoError(t, err)
		assert.Equal(t, "world", r)
	})

	t.Run("We should be able to eval an object expression", func(t *testing.T) {

		t.Run("We can use a map as variable", func(t *testing.T) {
			expression, err := NewExprExpression("${i.am.an.object}")

			assert.NoError(t, err)
			assert.Equal(t, "i.am.an.object", expression.Source())

			variables := map[string]any{
				"i": map[string]any{
					"am": map[string]any{
						"an": map[string]any{
							"object": "i am an object!",
						},
					},
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, "i am an object!", r)
		})

		t.Run("We can use a struct as variable", func(t *testing.T) {
			expression, err := NewExprExpression("${i.am.an.Object}")

			assert.NoError(t, err)
			assert.Equal(t, "i.am.an.Object", expression.Source())

			variables := map[string]any{
				"i": map[string]any{
					"am": map[string]any{
						"an": ObjectArg{
							Object: "i am an object!",
						},
					},
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, "i am an object!", r)
		})

		t.Run("We can use a map whose keys contains kebab-names", func(t *testing.T) {
			expression, err := NewExprExpression("${i['am-an'].object}")

			assert.NoError(t, err)
			assert.Equal(t, "i['am-an'].object", expression.Source())

			variables := map[string]any{
				"i": map[string]any{
					"am-an": map[string]any{
						"object": "i am an object!",
					},
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, "i am an object!", r)
		})

	})
}
