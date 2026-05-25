package controlplanecomponent

import (
	"testing"

	. "github.com/onsi/gomega"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestKonnectivityContainerBuildContainer_Socks5StartupProbe(t *testing.T) {
	t.Run("When building socks5 container with default port, it should have TCP startup probe on port 8090", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: Socks5,
			Socks5Options: Socks5Options{
				ResolveFromGuestClusterDNS: ptr.To(true),
			},
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.Name).To(Equal("konnectivity-proxy-socks5"))
		g.Expect(container.Image).To(Equal("test-image"))
		g.Expect(container.StartupProbe).ToNot(BeNil())
		g.Expect(container.StartupProbe.TCPSocket).ToNot(BeNil())
		g.Expect(container.StartupProbe.TCPSocket.Port).To(Equal(intstr.FromInt32(8090)))
		g.Expect(container.StartupProbe.InitialDelaySeconds).To(Equal(int32(5)))
		g.Expect(container.StartupProbe.PeriodSeconds).To(Equal(int32(2)))
		g.Expect(container.StartupProbe.FailureThreshold).To(Equal(int32(30)))
		g.Expect(container.StartupProbe.TimeoutSeconds).To(Equal(int32(1)))
	})

	t.Run("When building socks5 container with custom port, it should have TCP startup probe on custom port", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: Socks5,
			Socks5Options: Socks5Options{
				ServingPort: 9999,
			},
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.StartupProbe).ToNot(BeNil())
		g.Expect(container.StartupProbe.TCPSocket.Port).To(Equal(intstr.FromInt32(9999)))
	})
}

func TestKonnectivityContainerBuildContainer_HTTPSStartupProbe(t *testing.T) {
	t.Run("When building HTTPS container with default port, it should have TCP startup probe on port 8090", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: HTTPS,
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.Name).To(Equal("konnectivity-proxy-https"))
		g.Expect(container.StartupProbe).ToNot(BeNil())
		g.Expect(container.StartupProbe.TCPSocket.Port).To(Equal(intstr.FromInt32(8090)))
	})

	t.Run("When building HTTPS container with custom port, it should have TCP startup probe on custom port", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: HTTPS,
			HTTPSOptions: HTTPSOptions{
				ServingPort: 8092,
			},
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.StartupProbe.TCPSocket.Port).To(Equal(intstr.FromInt32(8092)))
	})
}

func TestKonnectivityContainerBuildContainer_Args(t *testing.T) {
	t.Run("When building socks5 container with resolve flags, it should include correct args", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: Socks5,
			Socks5Options: Socks5Options{
				KonnectivityHost:                "custom-host",
				KonnectivityPort:                9000,
				ServingPort:                     8888,
				ResolveFromGuestClusterDNS:      ptr.To(true),
				ResolveFromManagementClusterDNS: ptr.To(false),
				DisableResolver:                 ptr.To(false),
			},
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.Args).To(ContainElement("--konnectivity-hostname=custom-host"))
		g.Expect(container.Args).To(ContainElement("--konnectivity-port=9000"))
		g.Expect(container.Args).To(ContainElement("--serving-port=8888"))
		g.Expect(container.Args).To(ContainElement("--resolve-from-guest-cluster-dns=true"))
		g.Expect(container.Args).To(ContainElement("--resolve-from-management-cluster-dns=false"))
		g.Expect(container.Args).To(ContainElement("--disable-resolver=false"))
	})

	t.Run("When building HTTPS container with cloud API bypass, it should include correct args", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: HTTPS,
			HTTPSOptions: HTTPSOptions{
				ConnectDirectlyToCloudAPIs: ptr.To(true),
			},
		}

		hcp := &hyperv1.HostedControlPlane{}
		container := opts.buildContainer(hcp, "test-image", nil)

		g.Expect(container.Args).To(ContainElement("--connect-directly-to-cloud-apis=true"))
	})
}

func TestKonnectivityContainerInjectKonnectivityContainer(t *testing.T) {
	t.Run("When injecting socks5 container into podspec, it should add container and volumes", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: Socks5,
		}

		hcp := &hyperv1.HostedControlPlane{}
		podSpec := &corev1.PodSpec{}

		cpContext := ControlPlaneContext{
			HCP:                  hcp,
			ReleaseImageProvider: &fakeImageProvider{},
		}

		opts.injectKonnectivityContainer(cpContext, podSpec)

		g.Expect(podSpec.Containers).To(HaveLen(1))
		g.Expect(podSpec.Containers[0].Name).To(Equal("konnectivity-proxy-socks5"))
		g.Expect(podSpec.Containers[0].StartupProbe).ToNot(BeNil())

		g.Expect(podSpec.Volumes).To(HaveLen(2))
		g.Expect(podSpec.Volumes[0].Name).To(Equal("konnectivity-proxy-cert"))
		g.Expect(podSpec.Volumes[1].Name).To(Equal("konnectivity-proxy-ca"))
	})

	t.Run("When injecting Dual mode into podspec, it should add both HTTPS and Socks5 containers", func(t *testing.T) {
		g := NewGomegaWithT(t)

		opts := KonnectivityContainerOptions{
			Mode: Dual,
		}

		hcp := &hyperv1.HostedControlPlane{}
		podSpec := &corev1.PodSpec{}

		cpContext := ControlPlaneContext{
			HCP:                  hcp,
			ReleaseImageProvider: &fakeImageProvider{},
		}

		opts.injectKonnectivityContainer(cpContext, podSpec)

		g.Expect(podSpec.Containers).To(HaveLen(2))
		g.Expect(podSpec.Containers[0].Name).To(Equal("konnectivity-proxy-https"))
		g.Expect(podSpec.Containers[0].StartupProbe).ToNot(BeNil())
		g.Expect(podSpec.Containers[1].Name).To(Equal("konnectivity-proxy-socks5"))
		g.Expect(podSpec.Containers[1].StartupProbe).ToNot(BeNil())
	})
}

// fakeImageProvider implements ReleaseImageProvider for testing
type fakeImageProvider struct{}

func (f *fakeImageProvider) GetImage(key string) string {
	return "fake-image-" + key
}

func (f *fakeImageProvider) ImageExist(key string) (string, bool) {
	return "fake-image-" + key, true
}

func (f *fakeImageProvider) ComponentImages() map[string]string {
	return map[string]string{}
}

func (f *fakeImageProvider) ComponentVersions() (map[string]string, error) {
	return map[string]string{}, nil
}

func (f *fakeImageProvider) Version() string {
	return "4.16.0"
}
