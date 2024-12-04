package cel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Expression(t *testing.T) {

	t.Run("We should be able to eval a constant expression", func(t *testing.T) {
		expression, err := Parse(`${"sample"}`)

		assert.NoError(t, err)
		assert.Equal(t, `"sample"`, expression.Source())

		r, err := expression.Evaluate(NoArgs())

		assert.NoError(t, err)
		assert.Equal(t, "sample", r)
	})

	t.Run("We should be able to eval an array expression", func(t *testing.T) {
		expression, err := Parse(`${sample[1]}`)

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
		expression, err := Parse("${i.am.an.object}")

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

	t.Run("We should be able to eval a composite expression", func(t *testing.T) {
		expression, err := Parse(`hello ${"world"}!`)

		assert.NoError(t, err)
		assert.Equal(t, `hello ${"world"}!`, expression.Source())

		r, err := expression.Evaluate(NoArgs())

		assert.NoError(t, err)
		assert.Equal(t, "hello world!", r)

		t.Run("a bit more complex composite expression...", func(t *testing.T) {
			expression, err := Parse(`${"hello"}, ${"world"}!`)

			assert.NoError(t, err)
			assert.Equal(t, `${"hello"}, ${"world"}!`, expression.Source())

			r, err := expression.Evaluate(NoArgs())

			assert.NoError(t, err)
			assert.Equal(t, "hello, world!", r)
		})

		t.Run("a composite expression using objects", func(t *testing.T) {
			expression, err := Parse(`${"hello"}, ${"world"}. ${i.am.an.object}!`)

			assert.NoError(t, err)
			assert.Equal(t, `${"hello"}, ${"world"}. ${i.am.an.object}!`, expression.Source())

			variables := map[string]any{
				"i": map[string]any{
					"am": map[string]any{
						"an": map[string]any{
							"object": "i am an object",
						},
					},
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, "hello, world. i am an object!", r)
		})

		t.Run("a composite expression using arrays", func(t *testing.T) {
			expression, err := Parse("${message[0]}, ${message[1]}!")

			assert.NoError(t, err)
			assert.Equal(t, "${message[0]}, ${message[1]}!", expression.Source())

			variables := map[string]any{
				"message": []string{
					"hello",
					"world",
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, "hello, world!", r)
		})
	})
}
