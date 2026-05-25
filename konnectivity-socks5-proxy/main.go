package konnectivitysocks5proxy

import (
	"fmt"
	"net"
	"os"

	"github.com/openshift/hypershift/support/konnectivityproxy"
	"github.com/openshift/hypershift/support/supportedversion"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/armon/go-socks5"
	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
)

func NewStartCommand() *cobra.Command {
	l := log.Log.WithName("konnectivity-socks5-proxy")
	log.SetLogger(zap.New(zap.JSONEncoder(func(o *zapcore.EncoderConfig) {
		o.EncodeTime = zapcore.RFC3339TimeEncoder
	})))
	cmd := &cobra.Command{
		Use:   "konnectivity-socks5-proxy",
		Short: "Runs the konnectivity socks5 proxy server.",
		Long: ` Runs the konnectivity socks5 proxy server.
		This proxy accepts request and tunnels them through the designated Konnectivity Server.
		When resolving hostnames, the proxy will attempt to derive the Cluster IP Address from
		a Kubernetes Service using the provided KubeConfig. If the IP address
		cannot be resolved from a service, the system DNS is used to resolve hostnames.
		`,
	}

	opts := konnectivityproxy.Options{}

	var servingPort uint32

	cmd.Flags().StringVar(&opts.KonnectivityHost, "konnectivity-hostname", "konnectivity-server-local", "The hostname of the konnectivity service.")
	cmd.Flags().Uint32Var(&opts.KonnectivityPort, "konnectivity-port", 8090, "The konnectivity port that socks5 proxy should connect to.")
	cmd.Flags().Uint32Var(&servingPort, "serving-port", 8090, "The port that socks5 proxy should serve on.")
	cmd.Flags().BoolVar(&opts.ConnectDirectlyToCloudAPIs, "connect-directly-to-cloud-apis", false, "If true, traffic destined for AWS or Azure APIs should be sent there directly rather than going through konnectivity. If enabled, proxy env vars from the mgmt cluster must be propagated to this container")
	cmd.Flags().BoolVar(&opts.ResolveFromGuestClusterDNS, "resolve-from-guest-cluster-dns", false, "If DNS resolving should use the guest clusters cluster-dns")
	cmd.Flags().BoolVar(&opts.ResolveFromManagementClusterDNS, "resolve-from-management-cluster-dns", false, "If guest cluster's dns fails, fallback to the management cluster's dns")
	cmd.Flags().BoolVar(&opts.DisableResolver, "disable-resolver", false, "If true, DNS resolving is disabled. Takes precedence over resolve-from-guest-cluster-dns and resolve-from-management-cluster-dns")

	cmd.Flags().StringVar(&opts.CAFile, "ca-cert-path", "/etc/konnectivity/proxy-ca/ca.crt", "The path to the konnectivity client's ca-cert.")
	cmd.Flags().StringVar(&opts.ClientCertFile, "tls-cert-path", "/etc/konnectivity/proxy-client/tls.crt", "The path to the konnectivity client's tls certificate.")
	cmd.Flags().StringVar(&opts.ClientKeyFile, "tls-key-path", "/etc/konnectivity/proxy-client/tls.key", "The path to the konnectivity client's private key.")

	cmd.Run = func(cmd *cobra.Command, args []string) {
		l.Info("Starting proxy", "version", supportedversion.String())
		opts.Log = l

		ctx := signals.SetupSignalHandler()

		err := runWithCoreGuard(ctx, l, opts, servingPort, func(dialer konnectivityproxy.ProxyDialer) error {
			conf := &socks5.Config{
				Dial:     dialer.DialContext,
				Resolver: dialer,
			}
			server, err := socks5.New(conf)
			if err != nil {
				return fmt.Errorf("cannot create socks5 server: %w", err)
			}

			// Create listener explicitly for graceful shutdown
			listener, err := net.Listen("tcp", fmt.Sprintf(":%d", servingPort))
			if err != nil {
				return fmt.Errorf("cannot create listener: %w", err)
			}

			// Run server in goroutine
			serveDone := make(chan error, 1)
			go func() {
				serveDone <- server.Serve(listener)
			}()

			// Wait for either context cancellation or serve error
			select {
			case <-ctx.Done():
				// Graceful shutdown: close listener to stop accepting new connections
				_ = listener.Close()
				// Wait for Serve to finish
				<-serveDone
				// Treat context cancellation as clean shutdown
				return nil
			case err := <-serveDone:
				// Server stopped, return the error (could be from listener close or actual error)
				return err
			}
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	return cmd
}
