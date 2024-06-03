package e2e

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/cybozu-go/login-protector/test/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const namespace = "login-protector-system"

var _ = Describe("controller", Ordered, func() {
	BeforeAll(func() {
	})

	AfterAll(func() {
	})

	Context("Operator", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
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
			}
			EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())

		})

		It("should deploy target StatefulSet", func() {
			By("deploying target StatefulSet")
			_, err := utils.Kubectl(nil, "apply", "-f", "./test/testdata/statefulset.yaml")
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				statefulset := appsv1.StatefulSet{}
				err := utils.GetResource("", "teststs", &statefulset)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(statefulset.Status.ReadyReplicas).Should(BeNumerically("==", 2))
			}).Should(Succeed())
		})

		const configuredPollingIntervalSeconds = 5
		const testIntervalSeconds = configuredPollingIntervalSeconds + 2
		const testInterval = time.Second * time.Duration(testIntervalSeconds)

		It("should create PodDisruptionBudgets", func() {

			// prepare
			ptmx, err := pty.Start(exec.Command("bash"))
			Expect(err).NotTo(HaveOccurred())
			defer ptmx.Close()

			pdbList := &policyv1.PodDisruptionBudgetList{}
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")

			// a PDB should be created for teststs-0 because it is selected by `-l` option of the controller

			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "teststs-0", "-it", "--", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				if err != nil {
					panic(err)
				}
			}()

			time.Sleep(testInterval)

			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(HaveLen(1), "expected pdb does not exist")
			Expect(pdbList.Items[0].Name).Should(Equal("teststs-0"))
			Expect(pdbList.Items[0].Spec.Selector.MatchLabels["statefulset.kubernetes.io/pod-name"]).Should(Equal("teststs-0"))

			// the PDB should be deleted after the logout from the Pod

			time.Sleep(testInterval)

			fmt.Println("PDB should be deleted")
			pdbList = &policyv1.PodDisruptionBudgetList{}
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")

			// a PDB should not be created for teststs2-0 because it is not selected by `-l` option of the controller
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "teststs2-0", "-it", "--", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				if err != nil {
					panic(err)
				}
			}()

			time.Sleep(testInterval)

			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
		})
	})
})
