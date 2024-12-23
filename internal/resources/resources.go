package resources

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"

	"github.com/dominikbraun/graph"
	api "github.com/nubank/klaudio/api/v1alpha1"
	"github.com/nubank/klaudio/internal/expression"
	"github.com/nubank/klaudio/internal/refs"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	resourcesRe = regexp.MustCompile(`resources\.([^.]+)`)
)

type ResourceGroup struct {
	all map[string]*Resource
}

func (r ResourceGroup) Get(name string) (*Resource, error) {
	matches := resourcesRe.FindStringSubmatch(name)

	if len(matches) != 0 {
		name = matches[1]
	}

	resource, ok := r.all[name]
	if !ok {
		return nil, fmt.Errorf("resource %s is not registered", name)
	}
	return resource, nil
}

type ResourcePropertiesArgs struct {
	all map[string]any
}

func NewResourcePropertiesArgs(parameters map[string]any, refs *refs.References) *ResourcePropertiesArgs {
	variables := make(map[string]any)
	variables["parameters"] = parameters

	newRefs := make(map[string]any)
	for name, value := range refs.All() {
		newRefs[name] = value
	}
	variables["refs"] = newRefs

	return &ResourcePropertiesArgs{all: variables}
}

func (r *ResourcePropertiesArgs) WithResource(name string, resource *api.Resource) (*ResourcePropertiesArgs, error) {
	resources, ok := r.all["resources"].(map[string]any)
	if !ok {
		resources = make(map[string]any)
	}

	resourceAsJson, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	resourceAsMap := make(map[string]any)
	if err := json.Unmarshal(resourceAsJson, &resourceAsMap); err != nil {
		return nil, err
	}

	allProperties := make(map[string]any)
	if err := json.Unmarshal(resource.Spec.Properties.Raw, &allProperties); err != nil {
		return nil, err
	}

	if spec, isSafe := resourceAsMap["Spec"].(map[string]any); isSafe {
		if _, isSafe := spec["Properties"]; isSafe {
			resourceAsMap["Spec"].(map[string]any)["Properties"] = allProperties
		}
	}

	if outputs := resource.Status.Outputs; outputs != nil {
		allStatusOutputs := make(map[string]any)
		if err := json.Unmarshal(outputs.Raw, &allStatusOutputs); err != nil {
			return nil, err
		}

		if spec, isSafe := resourceAsMap["Status"].(map[string]any); isSafe {
			if _, isSafe := spec["Outputs"]; isSafe {
				resourceAsMap["Status"].(map[string]any)["Outputs"] = allStatusOutputs
			}
		}
	}

	resources[name] = resourceAsMap
	r.all["resources"] = resources

	return r, nil
}

type Resource struct {
	Name         string
	Ref          *api.ResourceRef
	properties   *ResourceProperties
	dependencies []string
}

func (r *Resource) Evaluate(args *ResourcePropertiesArgs) (ExpandedResourceProperties, error) {
	newProperties := make(map[string]any)
	for name, property := range r.properties.properties {
		expanded, err := property.Evaluate(args)
		if err != nil {
			return nil, err
		}
		newProperties[name] = expanded
	}

	return ExpandedResourceProperties(newProperties), nil
}

type ExpandedResourceProperties map[string]any

type ResourceProperties struct {
	properties   map[string]ResourceProperty
	dependencies []string
}

type ResourceProperty interface {
	Name() string
	Dependencies() []string
	Evaluate(*ResourcePropertiesArgs) (any, error)
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

func (p ObjectResourceProperty) Evaluate(args *ResourcePropertiesArgs) (any, error) {
	newMap := make(map[string]any)
	for name, property := range p.properties {
		newValue, err := property.Evaluate(args)
		if err != nil {
			return nil, err
		}
		newMap[name] = newValue
	}
	return newMap, nil
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

func (p ArrayResourceProperty) Evaluate(args *ResourcePropertiesArgs) (any, error) {
	newArray := make([]any, len(p.properties))
	for _, property := range p.properties {
		newValue, err := property.Evaluate(args)
		if err != nil {
			return nil, err
		}
		newArray = append(newArray, newValue)
	}
	return newArray, nil
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

func (p ExpressionResourceProperty) Evaluate(args *ResourcePropertiesArgs) (any, error) {
	return p.expression.Evaluate(args.all)
}

func NewResourceGroup() *ResourceGroup {
	return &ResourceGroup{all: make(map[string]*Resource)}
}

func (r *ResourceGroup) Graph() ([]string, error) {
	resourcesDag := graph.New(graph.StringHash, graph.Directed(), graph.PreventCycles())

	vertexNameFn := func(name string) string {
		return fmt.Sprintf("resources.%s", name)
	}

	for name := range maps.Keys(r.all) {
		err := resourcesDag.AddVertex(vertexNameFn(name))
		if err != nil {
			return nil, err
		}
	}

	for name, resource := range r.all {
		for _, dependency := range resource.dependencies {
			fmt.Printf("vertex %s, edge %s\n", name, dependency)
			err := resourcesDag.AddEdge(dependency, vertexNameFn(name))
			if err != nil {
				return nil, err
			}
		}
	}

	return graph.StableTopologicalSort(resourcesDag, func(a, b string) bool {
		return a < b
	})
}

func (r *ResourceGroup) NewResource(name string, properties *runtime.RawExtension) (*Resource, error) {
	if _, ok := r.all[name]; ok {
		return nil, fmt.Errorf("resource '%s' is duplicated; check the spec", name)
	}

	resource := &Resource{Name: name}
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
		expressionResourceProperty := &ExpressionResourceProperty{
			name:         name,
			expression:   e,
			dependencies: e.Dependencies(),
		}
		return expressionResourceProperty, nil
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
