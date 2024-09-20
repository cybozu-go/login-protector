package teleport

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/cybozu-go/login-protector/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
)

const namespace = "login-protector-system"

var _ = Describe("controller", Ordered, func() {
	Context("Operator", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			Eventually(func() error {
				// Get pod name
				pods := &corev1.PodList{}
				err := utils.GetResource(namespace, "", pods, "-l", "control-plane=controller-manager")
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if len(pods.Items) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(pods.Items))
				}
				controllerPod := pods.Items[0]
				ExpectWithOffset(2, controllerPod.Name).Should(ContainSubstring("controller-manager"))

				// Validate pod status
				if string(controllerPod.Status.Phase) != "Running" {
					return fmt.Errorf("controller pod in %s status", controllerPod.Status.Phase)
				}
				return nil
			}).Should(Succeed())
		})

		const configuredPollingIntervalSeconds = 5
		const testIntervalSeconds = configuredPollingIntervalSeconds + 2
		const testInterval = time.Second * time.Duration(testIntervalSeconds)

		It("should create PodDisruptionBudgets", func() {

			// prepare a PTY to run ssh
			ptmx, err := pty.Start(exec.Command("bash"))
			Expect(err).NotTo(HaveOccurred())
			defer ptmx.Close()

			// make sure no PDB exists
			pdbList := &policyv1.PodDisruptionBudgetList{}
			err = utils.GetResource("teleport", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")

			// login to node-demo-0 Pod using `tsh ssh`
			go func() {
				cmd := exec.Command("./teleport/tsh", "-i", "./identity", "--proxy", "localhost:3080", "--insecure", "ssh", "cybozu@node-demo-0", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				cmd.Stdin = ptmx
				_, err := utils.Run(cmd)
				if err != nil {
					panic(err)
				}
			}()

			Eventually(func(g Gomega) {
				// a PDB should be created for node-demo-0
				err = utils.GetResource("teleport", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(HaveLen(1), "expected pdb does not exist")
				g.Expect(pdbList.Items[0].Name).Should(Equal("node-demo-0"))
				g.Expect(pdbList.Items[0].Spec.Selector.MatchLabels["statefulset.kubernetes.io/pod-name"]).Should(Equal("node-demo-0"))
			}).WithTimeout(testInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				// the PDB should be deleted after the logout from the Pod
				pdbList = &policyv1.PodDisruptionBudgetList{}
				err = utils.GetResource("teleport", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
			}).WithTimeout(5 * time.Minute).Should(Succeed()) // Wait 5 minutes. Because it takes more than 3 minutes for teleport-session-tracker change the status.
		})
	})
})
