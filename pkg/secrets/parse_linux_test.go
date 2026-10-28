package secrets

import (
	"testing"

	"golang.org/x/sys/unix"
)

// The data plane compares Protocol with the kernel's protocol numbers.
func TestProtocolConstantsMatchKernel(t *testing.T) {
	if ProtocolTCP != unix.IPPROTO_TCP || ProtocolUDP != unix.IPPROTO_UDP {
		t.Fatalf("ProtocolTCP=%d ProtocolUDP=%d, kernel has %d/%d",
			ProtocolTCP, ProtocolUDP, unix.IPPROTO_TCP, unix.IPPROTO_UDP)
	}
}
