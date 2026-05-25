package konnectivitysocks5proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/openshift/hypershift/support/konnectivityproxy"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/go-logr/logr"
)

// mockProxyDialer implements konnectivityproxy.ProxyDialer interface
type mockProxyDialer struct{}

func (m *mockProxyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, nil
}

func (m *mockProxyDialer) Dial(network, address string) (net.Conn, error) {
	return nil, nil
}

func (m *mockProxyDialer) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, net.ParseIP("127.0.0.1"), nil
}

func (m *mockProxyDialer) IsCloudAPI(host string) bool {
	return false
}

func waitBackoffForTest() wait.Backoff {
	return wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   2.0,
		Cap:      50 * time.Millisecond,
	}
}

func TestIsTransientKonnectivityError(t *testing.T) {
	t.Run("When error is nil, it should not be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(nil)).To(BeFalse())
	})

	t.Run("When error is context deadline exceeded, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(context.DeadlineExceeded)).To(BeTrue())
	})

	t.Run("When error is connection refused, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(fmt.Errorf("dial tcp: connection refused"))).To(BeTrue())
	})

	t.Run("When error is syscall ECONNREFUSED, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.ECONNREFUSED)).To(BeTrue())
	})

	t.Run("When error is syscall ECONNRESET, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.ECONNRESET)).To(BeTrue())
	})

	t.Run("When error is syscall ETIMEDOUT, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.ETIMEDOUT)).To(BeTrue())
	})

	t.Run("When error is syscall EHOSTUNREACH, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.EHOSTUNREACH)).To(BeTrue())
	})

	t.Run("When error is syscall ENETUNREACH, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.ENETUNREACH)).To(BeTrue())
	})

	t.Run("When error is syscall EPIPE, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(syscall.EPIPE)).To(BeTrue())
	})

	t.Run("When error is network timeout, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		timeoutErr := &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}
		g.Expect(isTransientKonnectivityError(timeoutErr)).To(BeTrue())
	})

	t.Run("When error contains TLS handshake timeout, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("TLS handshake timeout"))).To(BeTrue())
	})

	t.Run("When error contains connection reset, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("connection reset by peer"))).To(BeTrue())
	})

	t.Run("When error contains no route to host, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("no route to host"))).To(BeTrue())
	})

	t.Run("When error contains network is unreachable, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("network is unreachable"))).To(BeTrue())
	})

	t.Run("When error is validation failure, it should not be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("failed validation: KonnectivityHost is required"))).To(BeFalse())
	})

	t.Run("When error is file not found, it should not be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(os.ErrNotExist)).To(BeFalse())
	})

	t.Run("When error is wrapped in OpError with syscall error, it should be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		opErr := &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		g.Expect(isTransientKonnectivityError(opErr)).To(BeTrue())
	})
}

func TestDialKonnectivityServerTCP(t *testing.T) {
	t.Run("When a local listener accepts connections, it should succeed", func(t *testing.T) {
		g := NewGomegaWithT(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		g.Expect(err).ToNot(HaveOccurred())
		defer ln.Close()

		_, portStr, err := net.SplitHostPort(ln.Addr().String())
		g.Expect(err).ToNot(HaveOccurred())

		var port uint32
		_, err = fmt.Sscanf(portStr, "%d", &port)
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(dialKonnectivityServerTCP(context.Background(), "127.0.0.1", port)).To(Succeed())
	})
}

func TestDialKonnectivityServerRetries(t *testing.T) {
	t.Run("When dial fails with a transient error then succeeds, it should eventually succeed", func(t *testing.T) {
		g := NewGomegaWithT(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		g.Expect(err).ToNot(HaveOccurred())
		defer ln.Close()

		_, portStr, err := net.SplitHostPort(ln.Addr().String())
		g.Expect(err).ToNot(HaveOccurred())

		var port uint32
		_, err = fmt.Sscanf(portStr, "%d", &port)
		g.Expect(err).ToNot(HaveOccurred())

		attempts := 0
		originalDial := dialKonnectivityServer
		dialKonnectivityServer = func(ctx context.Context, host string, dialPort uint32) error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("dial tcp: connection refused")
			}
			return originalDial(ctx, host, dialPort)
		}
		defer func() { dialKonnectivityServer = originalDial }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		g.Expect(retryDialKonnectivityServer(ctx, logr.Discard(), "127.0.0.1", port)).To(Succeed())
		g.Expect(attempts).To(BeNumerically(">=", 2))
	})
}

func retryDialKonnectivityServer(ctx context.Context, log logr.Logger, host string, port uint32) error {
	testBackoff := waitBackoffForTest()
	delay := testBackoff.DelayFunc()
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt++
		err := dialKonnectivityServer(ctx, host, port)
		if err == nil {
			return nil
		}
		if !isTransientKonnectivityError(err) {
			return err
		}
		log.Error(err, "transient konnectivity server dial failure, retrying", "attempt", attempt)
		time.Sleep(delay())
	}
}

func TestDialKonnectivityServerTCP_ConnectionRefused(t *testing.T) {
	t.Run("When port is not listening, it should return connection refused", func(t *testing.T) {
		g := NewGomegaWithT(t)
		// Try to connect to a port that's not listening (port 1 requires root)
		err := dialKonnectivityServerTCP(context.Background(), "127.0.0.1", 54321)
		g.Expect(err).To(HaveOccurred())
		g.Expect(isTransientKonnectivityError(err)).To(BeTrue())
	})

	t.Run("When context is canceled during dial, it should abort immediately", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Use a non-routable address (RFC 5737 TEST-NET-1) to ensure dial blocks
		// This guarantees the dial will timeout unless context cancellation works
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		start := time.Now()
		err := dialKonnectivityServerTCP(ctx, "192.0.2.1", 9999)
		elapsed := time.Since(start)

		g.Expect(err).To(HaveOccurred())
		// Should fail quickly (< 1 second) due to context cancellation, not wait for 5s timeout
		g.Expect(elapsed).To(BeNumerically("<", 1*time.Second))
		g.Expect(err.Error()).To(Or(ContainSubstring("context canceled"), ContainSubstring("operation was canceled")))
	})
}

func TestCoreGuardBackoff(t *testing.T) {
	t.Run("When using default backoff, it should have correct configuration", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Verify the default backoff configuration
		g.Expect(coreGuardBackoff.Duration).To(Equal(1 * time.Second))
		g.Expect(coreGuardBackoff.Factor).To(Equal(2.0))
		g.Expect(coreGuardBackoff.Jitter).To(Equal(0.1))
		g.Expect(coreGuardBackoff.Steps).To(Equal(5))
		g.Expect(coreGuardBackoff.Cap).To(Equal(30 * time.Second))
	})
}

func TestDefaultConstants(t *testing.T) {
	t.Run("When using default host and port, they should have correct values", func(t *testing.T) {
		g := NewGomegaWithT(t)

		g.Expect(defaultKonnectivityHost).To(Equal("konnectivity-server-local"))
		g.Expect(int(defaultKonnectivityPort)).To(Equal(8090))
		g.Expect(konnectivityDialTimeout).To(Equal(5 * time.Second))
	})
}

func TestRunWithCoreGuard(t *testing.T) {
	t.Run("When serve succeeds immediately, it should return nil", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalBootstrap := bootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			bootstrapKonnectivityFn = originalBootstrap
		}()

		mockDialer := &mockProxyDialer{}
		bootstrapKonnectivityFn = func(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return mockDialer, nil
		}

		serveCalled := false
		mockServe := func(_ konnectivityproxy.ProxyDialer) error {
			serveCalled = true
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := runWithCoreGuard(ctx, logr.Discard(), konnectivityproxy.Options{}, 8090, mockServe)

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(serveCalled).To(BeTrue())
	})

	t.Run("When bootstrap fails permanently, it should return error", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalBootstrap := bootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			bootstrapKonnectivityFn = originalBootstrap
		}()

		permanentErr := errors.New("fatal bootstrap error")
		bootstrapKonnectivityFn = func(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return nil, permanentErr
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := runWithCoreGuard(ctx, logr.Discard(), konnectivityproxy.Options{}, 8090, func(_ konnectivityproxy.ProxyDialer) error {
			return nil
		})

		g.Expect(err).To(Equal(permanentErr))
	})

	t.Run("When serve fails with transient error then succeeds, it should retry", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalBootstrap := bootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			bootstrapKonnectivityFn = originalBootstrap
		}()

		mockDialer := &mockProxyDialer{}
		bootstrapKonnectivityFn = func(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return mockDialer, nil
		}

		attempts := 0
		mockServe := func(_ konnectivityproxy.ProxyDialer) error {
			attempts++
			if attempts < 2 {
				return syscall.ECONNREFUSED
			}
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := runWithCoreGuard(ctx, logr.Discard(), konnectivityproxy.Options{}, 8090, mockServe)

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(attempts).To(Equal(2))
	})

	t.Run("When serve fails with permanent error, it should fail fast", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalBootstrap := bootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			bootstrapKonnectivityFn = originalBootstrap
		}()

		mockDialer := &mockProxyDialer{}
		bootstrapKonnectivityFn = func(ctx context.Context, log logr.Logger, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return mockDialer, nil
		}

		permanentErr := errors.New("invalid configuration")
		mockServe := func(_ konnectivityproxy.ProxyDialer) error {
			return permanentErr
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := runWithCoreGuard(ctx, logr.Discard(), konnectivityproxy.Options{}, 8090, mockServe)

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("invalid configuration"))
	})

	t.Run("When context is canceled during bootstrap, it should return context error", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalBootstrap := bootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			bootstrapKonnectivityFn = originalBootstrap
		}()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := runWithCoreGuard(ctx, logr.Discard(), konnectivityproxy.Options{}, 8090, func(_ konnectivityproxy.ProxyDialer) error {
			return nil
		})

		g.Expect(err).To(Equal(context.Canceled))
	})
}

func TestBootstrapKonnectivity(t *testing.T) {
	t.Run("When tryBootstrap succeeds immediately, it should return dialer", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalTry := tryBootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			tryBootstrapKonnectivityFn = originalTry
		}()

		mockDialer := &mockProxyDialer{}
		tryBootstrapKonnectivityFn = func(ctx context.Context, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return mockDialer, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		dialer, err := bootstrapKonnectivity(ctx, logr.Discard(), konnectivityproxy.Options{})

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(dialer).To(Equal(mockDialer))
	})

	t.Run("When tryBootstrap fails with transient error then succeeds, it should retry", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalTry := tryBootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			tryBootstrapKonnectivityFn = originalTry
		}()

		attempts := 0
		mockDialer := &mockProxyDialer{}
		tryBootstrapKonnectivityFn = func(ctx context.Context, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			attempts++
			if attempts < 3 {
				return nil, syscall.ECONNREFUSED
			}
			return mockDialer, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		dialer, err := bootstrapKonnectivity(ctx, logr.Discard(), konnectivityproxy.Options{})

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(dialer).To(Equal(mockDialer))
		g.Expect(attempts).To(Equal(3))
	})

	t.Run("When tryBootstrap fails with permanent error, it should fail fast", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalTry := tryBootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			tryBootstrapKonnectivityFn = originalTry
		}()

		permanentErr := os.ErrNotExist
		tryBootstrapKonnectivityFn = func(ctx context.Context, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			return nil, permanentErr
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		dialer, err := bootstrapKonnectivity(ctx, logr.Discard(), konnectivityproxy.Options{})

		g.Expect(err).To(HaveOccurred())
		g.Expect(dialer).To(BeNil())
		g.Expect(err.Error()).To(ContainSubstring("konnectivity bootstrap failed"))
	})

	t.Run("When context is canceled, it should return context error", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalTry := tryBootstrapKonnectivityFn
		coreGuardBackoff = waitBackoffForTest()
		defer func() {
			coreGuardBackoff = originalBackoff
			tryBootstrapKonnectivityFn = originalTry
		}()

		tryBootstrapKonnectivityFn = func(ctx context.Context, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			time.Sleep(100 * time.Millisecond)
			return nil, syscall.ECONNREFUSED
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		dialer, err := bootstrapKonnectivity(ctx, logr.Discard(), konnectivityproxy.Options{})

		g.Expect(err).To(Equal(context.Canceled))
		g.Expect(dialer).To(BeNil())
	})

	t.Run("When context is canceled during sleep, it should exit immediately", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Save originals
		originalBackoff := coreGuardBackoff
		originalTry := tryBootstrapKonnectivityFn
		// Use a backoff with long delays to ensure we're testing cancellation during sleep
		coreGuardBackoff = wait.Backoff{
			Duration: 5 * time.Second,
			Factor:   1.0,
			Jitter:   0.0,
			Steps:    1,
			Cap:      5 * time.Second,
		}
		defer func() {
			coreGuardBackoff = originalBackoff
			tryBootstrapKonnectivityFn = originalTry
		}()

		attempts := 0
		tryBootstrapKonnectivityFn = func(ctx context.Context, opts konnectivityproxy.Options) (konnectivityproxy.ProxyDialer, error) {
			attempts++
			return nil, syscall.ECONNREFUSED // Always fail with transient error
		}

		ctx, cancel := context.WithCancel(context.Background())

		// Start bootstrap in a goroutine
		errChan := make(chan error, 1)
		go func() {
			_, err := bootstrapKonnectivity(ctx, logr.Discard(), konnectivityproxy.Options{})
			errChan <- err
		}()

		// Wait a bit for first attempt to happen
		time.Sleep(50 * time.Millisecond)

		// Cancel context during sleep
		cancel()

		// Should exit quickly (much faster than the 5 second sleep)
		select {
		case err := <-errChan:
			g.Expect(err).To(Equal(context.Canceled))
			g.Expect(attempts).To(Equal(1)) // Should have only attempted once
		case <-time.After(1 * time.Second):
			g.Fail("Context cancellation did not interrupt sleep")
		}
	})
}
