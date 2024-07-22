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
				err := utils.GetResource("", "target-sts", &statefulset)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(statefulset.Status.ReadyReplicas).Should(BeNumerically("==", 2))
			}).Should(Succeed())
		})

		const configuredPollingIntervalSeconds = 5
		const testIntervalSeconds = configuredPollingIntervalSeconds + 2
		const testInterval = time.Second * time.Duration(testIntervalSeconds)

		It("should create PodDisruptionBudgets", func() {

			// prepare a PTY to run kubectl exec
			ptmx, err := pty.Start(exec.Command("bash"))
			Expect(err).NotTo(HaveOccurred())
			defer ptmx.Close()

			// make sure no PDB exists
			pdbList := &policyv1.PodDisruptionBudgetList{}
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")

			// login to target-sts-0 Pod using `kubectl exec`
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "target-sts-0", "-it", "--", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				if err != nil {
					panic(err)
				}
			}()

			// wait for login-protector to create PDB
			time.Sleep(testInterval)

			// a PDB should be created for target-sts-0 because it has the target label (`login-protector.cybozu.io/protect: "true"`)
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(HaveLen(1), "expected pdb does not exist")
			Expect(pdbList.Items[0].Name).Should(Equal("target-sts-0"))
			Expect(pdbList.Items[0].Spec.Selector.MatchLabels["statefulset.kubernetes.io/pod-name"]).Should(Equal("target-sts-0"))

			// wait for the PDB to be deleted
			time.Sleep(testInterval)

			// the PDB should be deleted after the logout from the Pod
			fmt.Println("PDB should be deleted")
			pdbList = &policyv1.PodDisruptionBudgetList{}
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")

			// login to not-target-sts-0 Pod using `kubectl exec`
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "not-target-sts-0", "-it", "--", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				if err != nil {
					panic(err)
				}
			}()

			// wait for login-protector
			time.Sleep(testInterval)

			// a PDB should not be created for not-target-sts-0 because it does not have the target label (`login-protector.cybozu.io/protect: "true"`)
			err = utils.GetResource("", "", pdbList, "--ignore-not-found")
			Expect(err).NotTo(HaveOccurred())
			Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
		})

		It("should stop updating the target statefulset", func() {
			// prepare a PTY to run kubectl exec
			ptmx, err := pty.Start(exec.Command("bash"))
			Expect(err).NotTo(HaveOccurred())
			defer ptmx.Close()

			// login to target-sts-0 Pod using `kubectl exec`
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "target-sts-0", "-it", "--", "sleep", fmt.Sprintf("%d", 2*testIntervalSeconds+2))
				if err != nil {
					panic(err)
				}
			}()

			// wait for login-protector to create PDB
			time.Sleep(testInterval)

			// update container image of target-sts
			_, err = utils.Kubectl(nil, "set", "image", "sts/target-sts", "main=ghcr.io/cybozu/ubuntu-debug:22.04")
			Expect(err).NotTo(HaveOccurred())

			// make sure the container image is not updated
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				err := utils.GetResource("", "target-sts-0", pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Status.ContainerStatuses[0].Image).ShouldNot(Equal("ghcr.io/cybozu/ubuntu-debug:22.04"))
			}).WithTimeout(testInterval).Should(Succeed())

			// wait for the PDB to be deleted
			time.Sleep(testInterval)

			// make sure the container image is updated
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				err := utils.GetResource("", "target-sts-0", pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Status.ContainerStatuses[0].Image).Should(Equal("ghcr.io/cybozu/ubuntu-debug:22.04"))
			}).Should(Succeed())
		})
	})
})
