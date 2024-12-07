package resource

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/dominikbraun/graph"
	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"github.com/nubank/klaudio/internal/expression"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
)

type ResourceGroup struct {
	all map[string]*Resource
}

func (r ResourceGroup) Get(name string) (*Resource, error) {
	resource, ok := r.all[name]
	if !ok {
		return nil, fmt.Errorf("resource %s is not registered", name)
	}
	return resource, nil
}

type Resource struct {
	Ref          *resourcesv1alpha1.ResourceRef
	properties   *ResourceProperties
	dependencies []string
}

type ResourceProperties struct {
	properties   map[string]ResourceProperty
	dependencies []string
}

type ResourceProperty interface {
	Name() string
	Dependencies() []string
}

type ObjectResourceProperty struct {
	name         string
	properties   map[string]ResourceProperty
	dependencies []string
}

func (p ObjectResourceProperty) Name() string {
	return p.name
}

func (p ObjectResourceProperty) Dependencies() []string {
	return p.dependencies
}

type ArrayResourceProperty struct {
	name         string
	properties   []ResourceProperty
	dependencies []string
}

func (p ArrayResourceProperty) Name() string {
	return p.name
}

func (p ArrayResourceProperty) Dependencies() []string {
	return p.dependencies
}

type ExpressionResourceProperty struct {
	name         string
	expression   expression.Expression
	dependencies []string
}

func (p ExpressionResourceProperty) Name() string {
	return p.name
}

func (p ExpressionResourceProperty) Dependencies() []string {
	return p.dependencies
}

func NewResourceGroup() *ResourceGroup {
	return &ResourceGroup{all: make(map[string]*Resource)}
}

func (r *ResourceGroup) Graph() ([]string, error) {
	resourcesDag := graph.New(graph.StringHash, graph.Directed(), graph.PreventCycles())

	for name := range maps.Keys(r.all) {
		err := resourcesDag.AddVertex(name)
		if err != nil {
			return nil, err
		}
	}

	for name, resource := range r.all {
		for _, dependency := range resource.dependencies {
			fmt.Printf("vertex %s, edge %s\n", name, dependency)
			err := resourcesDag.AddEdge(dependency, name)
			if err != nil {
				return nil, err
			}
		}
	}

	return graph.StableTopologicalSort(resourcesDag, func(a, b string) bool {
		return a < b
	})
}

func (r *ResourceGroup) Add(name string, properties *runtime.RawExtension) (*Resource, error) {
	if _, ok := r.all[name]; ok {
		return nil, fmt.Errorf("resource '%s' is duplicated; check the spec", name)
	}

	resource := &Resource{}
	r.all[name] = resource

	if properties != nil {
		propertiesToExpressions := make(map[string]any)
		if err := json.Unmarshal(properties.Raw, &propertiesToExpressions); err != nil {
			return nil, fmt.Errorf("unable to unmarshall properties: %w", err)
		}

		resourcePropertiesAsExpressions, err := newResourceProperties(propertiesToExpressions)
		if err != nil {
			return nil, fmt.Errorf("unable to read resource properties from %s: %w", name, err)
		}
		resource.properties = resourcePropertiesAsExpressions
		resource.dependencies = resourcePropertiesAsExpressions.dependencies

	}
	return resource, nil
}

func newResourceProperties(properties map[string]any) (*ResourceProperties, error) {
	propertiesWithExpressions := make(map[string]ResourceProperty)
	dependencies := sets.NewString()

	for name, value := range properties {
		elementWithExpressions, err := readProperty(name, value)
		if err != nil {
			return nil, fmt.Errorf("unable to read properties from field %s: %w", name, err)
		}
		propertiesWithExpressions[name] = elementWithExpressions

		dependencies = dependencies.Insert(elementWithExpressions.Dependencies()...)
	}

	resourceProperties := &ResourceProperties{
		properties:   propertiesWithExpressions,
		dependencies: dependencies.List(),
	}

	return resourceProperties, nil
}

func readDependencies(value any) []string {
	dependencies := make([]string, 0)
	switch value := value.(type) {
	case map[string]any:
		for _, element := range value {
			dependencies = append(dependencies, readDependencies(element)...)
		}
	case []expression.Expression:
		for _, e := range value {
			dependencies = append(dependencies, e.Dependencies()...)
		}
	case expression.Expression:
		dependencies = append(dependencies, value.Dependencies()...)
	}

	return dependencies
}

func readProperty(name string, value any) (ResourceProperty, error) {
	switch value := value.(type) {
	case map[string]any:
		return readObjectProperty(name, value)
	case []any:
		return readArrayProperty(name, value)
	default:
		e, err := expression.Parse(value)
		if err != nil {
			return nil, err
		}
		scalarResourceProperty := &ExpressionResourceProperty{
			name:         name,
			expression:   e,
			dependencies: e.Dependencies(),
		}
		return scalarResourceProperty, nil
	}
}

func readObjectProperty(name string, value map[string]any) (ResourceProperty, error) {
	properties := make(map[string]ResourceProperty)
	dependencies := make([]string, 0)
	for propertyName, element := range value {
		newElement, err := readProperty(fmt.Sprintf("%s.%s", name, propertyName), element)
		if err != nil {
			return nil, err
		}
		properties[propertyName] = newElement
		dependencies = append(dependencies, newElement.Dependencies()...)
	}
	objectResourceProperty := &ObjectResourceProperty{
		name:         name,
		properties:   properties,
		dependencies: dependencies,
	}
	return objectResourceProperty, nil
}

func readArrayProperty(name string, value []any) (ResourceProperty, error) {
	values := make([]ResourceProperty, len(value))
	dependencies := make([]string, 0)
	for i, element := range value {
		newElement, err := readProperty(fmt.Sprintf("%s[%d]", name, i), element)
		if err != nil {
			return nil, err
		}
		values[i] = newElement
		dependencies = append(dependencies, newElement.Dependencies()...)
	}
	arrayResourceProperty := &ArrayResourceProperty{
		name:         name,
		properties:   values,
		dependencies: dependencies,
	}
	return arrayResourceProperty, nil
}
