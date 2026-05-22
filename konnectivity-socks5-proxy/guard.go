package konnectivitysocks5proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/openshift/hypershift/support/konnectivityproxy"

	"k8s.io/apimachinery/pkg/util/wait"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
)

const (
	defaultKonnectivityHost = "konnectivity-server-local"
	defaultKonnectivityPort = 8090
	konnectivityDialTimeout = 5 * time.Second
)

var (
	coreGuardBackoff = wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
		Cap:      30 * time.Second,
	}
	dialKonnectivityServer = dialKonnectivityServerTCP
)

// runWithCoreGuard keeps the process alive while konnectivity infrastructure becomes
// reachable, retrying transient failures with exponential backoff instead of exiting.
func runWithCoreGuard(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options, servingPort uint32, serve func(dialer konnectivityproxy.ProxyDialer) error) error {
	delay := coreGuardBackoff.DelayFunc()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		dialer, err := bootstrapKonnectivity(ctx, log, opts)
		if err != nil {
			return err
		}

		log.Info("Konnectivity socks5 proxy bootstrap complete, starting server", "port", servingPort)
		if err := serve(dialer); err != nil {
			if !isTransientKonnectivityError(err) {
				return fmt.Errorf("socks5 server exited: %w", err)
			}
			log.Error(err, "socks5 server stopped due to transient error, retrying")
			time.Sleep(delay())
			continue
		}
		return nil
	}
}

func bootstrapKonnectivity(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
	delay := coreGuardBackoff.DelayFunc()
	attempt := 0

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt++

		d, err := tryBootstrapKonnectivity(opts)
		if err == nil {
			return d, nil
		}
		if !isTransientKonnectivityError(err) {
			return nil, fmt.Errorf("konnectivity bootstrap failed: %w", err)
		}

		log.Error(err, "transient konnectivity bootstrap failure, retrying", "attempt", attempt)
		time.Sleep(delay())
	}
}

func tryBootstrapKonnectivity(opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot get client config: %w", err)
	}

	kubeClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("cannot get client: %w", err)
	}

	opts.Client = kubeClient

	dialer, err := konnectivityproxy.NewKonnectivityDialer(opts)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize konnectivity dialer: %w", err)
	}

	host := opts.KonnectivityHost
	if host == "" {
		host = defaultKonnectivityHost
	}
	port := opts.KonnectivityPort
	if port == 0 {
		port = defaultKonnectivityPort
	}

	if err := dialKonnectivityServer(host, port); err != nil {
		return nil, err
	}

	return dialer, nil
}

func dialKonnectivityServerTCP(host string, port uint32) error {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", address, konnectivityDialTimeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func isTransientKonnectivityError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if isTransientKonnectivityError(opErr.Err) {
			return true
		}
	}

	var syscallErr syscall.Errno
	if errors.As(err, &syscallErr) {
		switch syscallErr {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ETIMEDOUT,
			syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EPIPE:
			return true
		}
	}

	// Configuration errors such as missing certificate files should fail fast, not retry.
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	msg := err.Error()
	transientFragments := []string{
		"connection refused",
		"connection reset",
		"i/o timeout",
		"context deadline exceeded",
		"no route to host",
		"network is unreachable",
		"operation timed out",
		"TLS handshake timeout",
	}
	for _, fragment := range transientFragments {
		if strings.Contains(msg, fragment) {
			return true
		}
	}

	return false
}
