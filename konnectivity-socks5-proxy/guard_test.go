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

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/go-logr/logr"
)

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

	t.Run("When error is validation failure, it should not be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(errors.New("failed validation: KonnectivityHost is required"))).To(BeFalse())
	})

	t.Run("When error is file not found, it should not be transient", func(t *testing.T) {
		g := NewGomegaWithT(t)
		g.Expect(isTransientKonnectivityError(os.ErrNotExist)).To(BeFalse())
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

		g.Expect(dialKonnectivityServerTCP("127.0.0.1", port)).To(Succeed())
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
		dialKonnectivityServer = func(host string, dialPort uint32) error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("dial tcp: connection refused")
			}
			return originalDial(host, dialPort)
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
		err := dialKonnectivityServer(host, port)
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
