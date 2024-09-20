package teleport

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Run e2e tests with teleport using the Ginkgo runner.
func TestE2ETeleport(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting login-protector with teleport suite\n")
	SetDefaultEventuallyTimeout(30 * time.Second)
	SetDefaultEventuallyPollingInterval(1 * time.Second)
	RunSpecs(t, "e2e teleport suite")
}
