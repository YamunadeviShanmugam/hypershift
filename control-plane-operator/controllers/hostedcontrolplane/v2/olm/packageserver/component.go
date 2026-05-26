package packageserver

import (
	konnectivityagent "github.com/openshift/hypershift/control-plane-operator/controllers/hostedcontrolplane/v2/konnectivity_agent"
	component "github.com/openshift/hypershift/support/controlplane-component"
	"github.com/openshift/hypershift/support/podspec"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ComponentName = "packageserver"
)

var _ component.ComponentOptions = &packageServer{}

type packageServer struct{}

// IsRequestServing implements controlplanecomponent.ComponentOptions.
func (r *packageServer) IsRequestServing() bool {
	return false
}

// MultiZoneSpread implements controlplanecomponent.ComponentOptions.
func (r *packageServer) MultiZoneSpread() bool {
	return true
}

// NeedsManagementKASAccess implements controlplanecomponent.ComponentOptions.
func (r *packageServer) NeedsManagementKASAccess() bool {
	return false
}

func NewComponent() component.ControlPlaneComponent {
	return component.NewDeploymentComponent(ComponentName, &packageServer{}).
		WithAdaptFunction(adaptDeployment).
		WithDependencies(konnectivityagent.ComponentName).
		InjectKonnectivityContainer(component.KonnectivityContainerOptions{
			Mode: component.Socks5,
		}).
		InjectAvailabilityProberContainer(podspec.AvailabilityProberOpts{
			KubeconfigVolumeName: "kubeconfig",
			RequiredAPIs: []schema.GroupVersionKind{
				{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "CatalogSource"},
			},
		}).
		Build()
}
