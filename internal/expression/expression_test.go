package expression

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Expression(t *testing.T) {

	t.Run("We should be able to eval a constant expression", func(t *testing.T) {
		expression, err := Parse(`${"sample"}`)

		assert.NoError(t, err)
		assert.Equal(t, `"sample"`, expression.Source())

		r, err := expression.Evaluate()

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

		t.Run("We can use a map as dependency", func(t *testing.T) {
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

		t.Run("We can use a struct as dependency", func(t *testing.T) {
			expression, err := Parse("${i.am.an.object}")

			assert.NoError(t, err)
			assert.Equal(t, "i.am.an.object", expression.Source())

			type object struct {
				Object string `expr:"object"`
			}

			objectValue := object{
				Object: "i am an object!",
			}

			variables := map[string]any{
				"i": map[string]any{
					"am": map[string]any{
						"an": objectValue,
					},
				},
			}

			r, err := expression.Evaluate(variables)

			assert.NoError(t, err)
			assert.Equal(t, objectValue.Object, r)
		})

	})

	t.Run("We should be able to eval a composite expression", func(t *testing.T) {
		expression, err := Parse(`hello ${"world"}!`)

		assert.NoError(t, err)
		assert.Equal(t, `hello ${"world"}!`, expression.Source())

		r, err := expression.Evaluate()

		assert.NoError(t, err)
		assert.Equal(t, "hello world!", r)

		t.Run("a bit more complex composite expression...", func(t *testing.T) {
			expression, err := Parse(`${"hello"}, ${"world"}!`)

			assert.NoError(t, err)
			assert.Equal(t, `${"hello"}, ${"world"}!`, expression.Source())

			r, err := expression.Evaluate()

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

func Test_ExpressionDependencies(t *testing.T) {
	t.Run("We should be able to read dependencies from a cel expression", func(t *testing.T) {

		t.Run("We are looking for resources dependencies...", func(t *testing.T) {
			expression, err := Parse(`${resources.sample.whatever}`)

			assert.NoError(t, err)

			dependencies := expression.Dependencies()

			assert.Equal(t, []string{"resources.sample"}, dependencies)
		})

		t.Run("and for refs dependencies.", func(t *testing.T) {
			expression, err := Parse(`${refs.sample.whatever}`)

			assert.NoError(t, err)

			dependencies := expression.Dependencies()

			assert.Equal(t, []string{"refs.sample"}, dependencies)
		})

		t.Run("Constant cel expressions doesn't have dependencies.", func(t *testing.T) {
			expression, err := Parse(`${"hello"}`)

			assert.NoError(t, err)

			dependencies := expression.Dependencies()

			assert.Empty(t, dependencies)
		})
	})

}
