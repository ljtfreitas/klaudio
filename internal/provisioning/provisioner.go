package provisioning

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Provisioner interface {
	Run(ctx context.Context, resource *resourcesv1alpha1.Resource) (*ProvisionedResourceStatus, error)
}

type ProvisionerFactory func(client.Client, *dynamic.DynamicClient, *runtime.Scheme, logr.Logger, *resourcesv1alpha1.ResourceRefProvisioner) (Provisioner, error)

func SelectByName(name string) (ProvisionerFactory, error) {
	switch name {
	case PulumiProvisionerName:
		return newPulumiProvisioner, nil
	case OpenTofuProvisionerName:
		return newOpenTofuProvisioner, nil

	default:
		return nil, fmt.Errorf("unsupported provisioner: %s", name)
	}

}
