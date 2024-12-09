package refs

import (
	"context"
	"fmt"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type References struct {
	all map[string]ReferenceObject
}

func NewReferences() *References {
	return &References{all: make(map[string]ReferenceObject)}
}

type ReferenceObject interface{}

type ReferenceValue any

func (r *References) Add(ctx context.Context, client client.Client, ref resourcesv1alpha1.ResourceGroupRef) (ReferenceObject, error) {
	unknown := &unstructured.Unstructured{}
	groupVersion, err := schema.ParseGroupVersion(ref.ApiVersion)
	if err != nil {
		return nil, err
	}
	unknown.SetGroupVersionKind(groupVersion.WithKind(string(ref.Kind)))

	objectKey := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}

	if err := client.Get(ctx, objectKey, unknown); err != nil {
		return nil, fmt.Errorf("unable to find an ref %s from kind %s in namespace %s: %w", ref.Name, ref.Kind, ref.Namespace, err)
	}

	value := ReferenceValue(unknown.Object)

	r.all[ref.Name] = value

	return value, nil
}
