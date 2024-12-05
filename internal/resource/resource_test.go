package resource

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_ResourcesWithoutDependencies(t *testing.T) {

	resourceGroup := NewResourceGroup()

	sourceProperties := map[string]any{
		"object": map[string]any{
			"field": "value",
		},
		"array": []any{
			"value0",
			"value1",
			"value2",
		},
		"scalar": "value",
	}

	propertiesAsBytes, err := json.Marshal(sourceProperties)
	assert.NoError(t, err)

	err = resourceGroup.Add("my-resource", &runtime.RawExtension{Raw: propertiesAsBytes})
	assert.NoError(t, err)

	assert.Len(t, resourceGroup.all, 1)

	resource := resourceGroup.all["my-resource"]
	assert.NotNil(t, resource)

	resourceProperties := resource.properties
	assert.Len(t, resourceProperties.properties, len(sourceProperties))
	assert.Empty(t, resourceProperties.dependencies)

	t.Run("We should be able to generate a object property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["object"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "object", resourceProperty.Name())
		assert.Empty(t, resourceProperty.Dependencies())

		objectResourceProperty := resourceProperty.(*ObjectResourceProperty)

		t.Run("...and navigating through the nested fields", func(t *testing.T) {
			field := objectResourceProperty.properties["field"]

			assert.NotNil(t, field)
			assert.Equal(t, "object.field", field.Name())

			scalarProperty := field.(*ExpressionResourceProperty)
			assert.Equal(t, "object.field", scalarProperty.Name())

			expression := scalarProperty.expression
			assert.NotNil(t, expression)
			assert.Equal(t, "value", expression.Source())
		})
	})

	t.Run("We should be able to generate an array property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["array"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "array", resourceProperty.Name())
		assert.Empty(t, resourceProperty.Dependencies())

		arrayResourceProperty := resourceProperty.(*ArrayResourceProperty)

		t.Run("...and navigating through the nested values", func(t *testing.T) {
			values := arrayResourceProperty.properties

			for i, value := range values {
				assert.Equal(t, fmt.Sprintf("array[%d]", i), value.Name())

				scalarProperty := value.(*ExpressionResourceProperty)
				assert.Equal(t, fmt.Sprintf("array[%d]", i), scalarProperty.Name())

				expression := scalarProperty.expression
				assert.NotNil(t, expression)
				assert.Equal(t, fmt.Sprintf("value%d", i), expression.Source())
			}
		})
	})

	t.Run("We should be able to generate a simple, scalar property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["scalar"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "scalar", resourceProperty.Name())
		assert.Empty(t, resourceProperty.Dependencies())

		scalarProperty := resourceProperty.(*ExpressionResourceProperty)

		assert.Equal(t, "scalar", scalarProperty.Name())

		expression := scalarProperty.expression
		assert.NotNil(t, expression)
		assert.Equal(t, "value", expression.Source())
	})

}

func Test_ResourcesWithDependencies(t *testing.T) {

	resourceGroup := NewResourceGroup()

	sourceProperties := map[string]any{
		"object": map[string]any{
			"field": "${resources.other.whatever}",
		},
		"array": []any{
			"${resources.other.whatever}",
		},
		"scalar": "${resources.other.whatever}",
	}

	propertiesAsBytes, err := json.Marshal(sourceProperties)
	assert.NoError(t, err)

	err = resourceGroup.Add("my-resource", &runtime.RawExtension{Raw: propertiesAsBytes})
	assert.NoError(t, err)

	assert.Len(t, resourceGroup.all, 1)

	resource := resourceGroup.all["my-resource"]
	assert.NotNil(t, resource)

	resourceProperties := resource.properties
	assert.Len(t, resourceProperties.properties, len(sourceProperties))
	assert.Len(t, resourceProperties.dependencies, 1)

	t.Run("We should be able to generate a object property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["object"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "object", resourceProperty.Name())
		assert.Len(t, resourceProperty.Dependencies(), 1)

		objectResourceProperty := resourceProperty.(*ObjectResourceProperty)

		t.Run("...and navigating through the nested fields", func(t *testing.T) {
			field := objectResourceProperty.properties["field"]

			assert.NotNil(t, field)
			assert.Equal(t, "object.field", field.Name())

			expressionResourceProperty := field.(*ExpressionResourceProperty)
			assert.Equal(t, "object.field", expressionResourceProperty.Name())
			assert.Equal(t, []string{"other"}, expressionResourceProperty.dependencies)

			expression := expressionResourceProperty.expression
			assert.NotNil(t, expression)
			assert.Equal(t, "resources.other.whatever", expression.Source())
		})
	})

	t.Run("We should be able to generate an array property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["array"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "array", resourceProperty.Name())
		assert.Len(t, resourceProperty.Dependencies(), 1)

		arrayResourceProperty := resourceProperty.(*ArrayResourceProperty)

		t.Run("...and navigating through the nested values", func(t *testing.T) {
			values := arrayResourceProperty.properties

			for i, value := range values {
				assert.Equal(t, fmt.Sprintf("array[%d]", i), value.Name())

				expressionResourceProperty := value.(*ExpressionResourceProperty)
				assert.Equal(t, fmt.Sprintf("array[%d]", i), expressionResourceProperty.Name())
				assert.Equal(t, []string{"other"}, expressionResourceProperty.dependencies)

				expression := expressionResourceProperty.expression
				assert.NotNil(t, expression)
				assert.Equal(t, "resources.other.whatever", expression.Source())
			}
		})
	})

	t.Run("We should be able to generate a simple, scalar property", func(t *testing.T) {
		resourceProperty := resourceProperties.properties["scalar"]
		assert.NotNil(t, resourceProperty)
		assert.Equal(t, "scalar", resourceProperty.Name())
		assert.Len(t, resourceProperty.Dependencies(), 1)

		expressionResourceProperty := resourceProperty.(*ExpressionResourceProperty)

		assert.Equal(t, "scalar", expressionResourceProperty.Name())
		assert.Equal(t, []string{"other"}, expressionResourceProperty.dependencies)

		expression := expressionResourceProperty.expression
		assert.NotNil(t, expression)
		assert.Equal(t, "resources.other.whatever", expression.Source())
	})
}

func Test_ResourcesMustBeUnique(t *testing.T) {

	resourceGroup := NewResourceGroup()

	err := resourceGroup.Add("my-resource", nil)
	assert.NoError(t, err)

	err = resourceGroup.Add("my-resource", nil)
	assert.Error(t, err)
}

func Test_ResourcesGraph(t *testing.T) {
	resourceGroup := NewResourceGroup()

	// no dependencies
	err := resourceGroup.Add("resource-one", nil)
	assert.NoError(t, err)

	sourcePropertiesFromResourceTwo := map[string]any{
		"field": "${resources.resource-one.value}",
	}

	propertiesAsBytes, err := json.Marshal(sourcePropertiesFromResourceTwo)
	assert.NoError(t, err)

	// depends on resource-one
	err = resourceGroup.Add("resource-two", &runtime.RawExtension{Raw: propertiesAsBytes})
	assert.NoError(t, err)

	sourcePropertiesFromResourceThree := map[string]any{
		"field": "${resources.resource-two.value}",
	}

	propertiesAsBytes, err = json.Marshal(sourcePropertiesFromResourceThree)
	assert.NoError(t, err)

	// depends on resource-two
	err = resourceGroup.Add("resource-three", &runtime.RawExtension{Raw: propertiesAsBytes})
	assert.NoError(t, err)

	sourcePropertiesFromResourceFour := map[string]any{
		"field": "${resources.resource-one.value}",
	}

	propertiesAsBytes, err = json.Marshal(sourcePropertiesFromResourceFour)
	assert.NoError(t, err)

	// depends on resource-one
	err = resourceGroup.Add("resource-four", &runtime.RawExtension{Raw: propertiesAsBytes})
	assert.NoError(t, err)

	// no dependencies
	err = resourceGroup.Add("resource-five", nil)
	assert.NoError(t, err)

	dag, err := resourceGroup.Graph()
	assert.NoError(t, err)

	assert.Equal(t, []string{"resource-five", "resource-one", "resource-four", "resource-two", "resource-three"}, dag)
}
