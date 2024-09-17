package e2e

import (
	"fmt"
	"net/http"
	"os/exec"
	"reflect"
	"time"

	"github.com/creack/pty"
	"github.com/cybozu-go/login-protector/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	appsv1 "k8s.io/api/apps/v1"
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

			Eventually(func(g Gomega) {
				// a PDB should be created for target-sts-0 because it has the target label (`login-protector.cybozu.io/protect: "true"`)
				err = utils.GetResource("", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(HaveLen(1), "expected pdb does not exist")
				g.Expect(pdbList.Items[0].Name).Should(Equal("target-sts-0"))
				g.Expect(pdbList.Items[0].Spec.Selector.MatchLabels["statefulset.kubernetes.io/pod-name"]).Should(Equal("target-sts-0"))
			}).WithTimeout(testInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				// the PDB should be deleted after the logout from the Pod
				pdbList = &policyv1.PodDisruptionBudgetList{}
				err = utils.GetResource("", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
			}).WithTimeout(testInterval * 2).Should(Succeed())

			// login to not-target-sts-0 Pod using `kubectl exec`
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "not-target-sts-0", "-it", "--", "sleep", fmt.Sprintf("%d", testIntervalSeconds))
				if err != nil {
					panic(err)
				}
			}()

			Consistently(func(g Gomega) {
				// a PDB should not be created for not-target-sts-0 because it does not have the target label (`login-protector.cybozu.io/protect: "true"`)
				err = utils.GetResource("", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
			}).WithTimeout(testInterval).Should(Succeed())
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

			Eventually(func(g Gomega) {
				// a PDB should be created for target-sts-0
				pdbList := &policyv1.PodDisruptionBudgetList{}
				err = utils.GetResource("", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(HaveLen(1), "expected pdb does not exist")
			}).WithTimeout(testInterval).Should(Succeed())

			// update container image of target-sts
			_, err = utils.Kubectl(nil, "set", "image", "sts/target-sts", "main=ghcr.io/cybozu/ubuntu-debug:22.04")
			Expect(err).NotTo(HaveOccurred())

			// make sure the container image is not updated
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				err := utils.GetResource("", "target-sts-0", pod)
				g.Expect(err).NotTo(HaveOccurred())
				for _, c := range pod.Spec.Containers {
					if c.Name == "main" {
						g.Expect(c.Image).ShouldNot(Equal("ghcr.io/cybozu/ubuntu-debug:22.04"))
					}
				}
			}).WithTimeout(testInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				// the PDB should be deleted after the logout from the Pod
				pdbList := &policyv1.PodDisruptionBudgetList{}
				err = utils.GetResource("", "", pdbList, "--ignore-not-found")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pdbList.Items).Should(BeEmpty(), "unexpected pdb exists")
			}).WithTimeout(testInterval * 2).Should(Succeed())

			// make sure the container image is updated
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				err := utils.GetResource("", "target-sts-0", pod)
				g.Expect(err).NotTo(HaveOccurred())
				for _, c := range pod.Spec.Containers {
					if c.Name == "main" {
						g.Expect(c.Image).Should(Equal("ghcr.io/cybozu/ubuntu-debug:22.04"))
					}
				}
			}).WithTimeout(3 * time.Minute).Should(Succeed())
		})

		getMetrics := func(url string) map[string]*dto.MetricFamily {
			resp, err := http.Get(url)
			ExpectWithOffset(1, err).ShouldNot(HaveOccurred())
			defer resp.Body.Close()
			ExpectWithOffset(1, resp.StatusCode).Should(Equal(http.StatusOK))
			parser := expfmt.TextParser{}
			mfmap, err := parser.TextToMetricFamilies(resp.Body)
			ExpectWithOffset(1, err).ShouldNot(HaveOccurred())
			return mfmap
		}
		findMetric := func(metricFamilies map[string]*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
			mf := metricFamilies[name]
			for _, m := range mf.Metric {
				currentLabels := make(map[string]string)
				for _, p := range m.Label {
					currentLabels[*p.Name] = *p.Value
				}
				if reflect.DeepEqual(currentLabels, labels) {
					return m
				}
			}
			return nil
		}

		It("should export metrics", func() {
			By("prepare to access metrics endpoint")
			pods := &corev1.PodList{}
			err := utils.GetResource(namespace, "", pods, "-l", "control-plane=controller-manager")
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).Should(HaveLen(1))

			cmd := exec.Command("kubectl", "port-forward", "-n", namespace, pods.Items[0].Name, "8080:8080")
			go func() {
				err = cmd.Run()
				if err != nil {
					panic(err)
				}
			}()
			time.Sleep(1 * time.Second) // wait for port-forward to be ready
			defer cmd.Process.Kill()

			Eventually(func(g Gomega) {
				// make sure all metrics are "0"
				metrics := getMetrics("http://localhost:8080/metrics")
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_watcher_errors_total", map[string]string{"watcher": "local-session-watcher"})).ShouldNot(BeNil())
			}).WithTimeout(testInterval).Should(Succeed())

			ptmx, err := pty.Start(exec.Command("bash"))
			Expect(err).NotTo(HaveOccurred())
			defer ptmx.Close()

			// Wait for target-sts-0 Pod to be running
			Eventually(func(g Gomega) {
				var pod corev1.Pod
				err := utils.GetResource("", "target-sts-0", &pod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pod.Status.Phase).Should(Equal(corev1.PodRunning))
			}).Should(Succeed())

			// login to target-sts-0 Pod using `kubectl exec`
			go func() {
				_, err := utils.Kubectl(ptmx, "exec", "target-sts-0", "-it", "--", "sleep", fmt.Sprintf("%d", 3*testIntervalSeconds+2))
				if err != nil {
					panic(err)
				}
			}()

			Eventually(func(g Gomega) {
				// make sure protecting metrics for target-sts-0 is "1"
				metrics := getMetrics("http://localhost:8080/metrics")
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(1))
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_watcher_errors_total", map[string]string{"watcher": "local-session-watcher"})).ShouldNot(BeNil())
			}).WithTimeout(testInterval).Should(Succeed())

			// update container image of target-sts
			_, err = utils.Kubectl(nil, "set", "image", "sts/target-sts", "main=ghcr.io/cybozu/ubuntu-dev:22.04")
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				// make sure pending metrics for target-sts-0 is "1"
				metrics := getMetrics("http://localhost:8080/metrics")
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(1))
				g.Expect(findMetric(metrics, "login_protector_pod_protecting", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-0"}).GetGauge().GetValue()).Should(BeEquivalentTo(1))
				g.Expect(findMetric(metrics, "login_protector_pod_pending_updates", map[string]string{"namespace": "default", "pod": "target-sts-1"}).GetGauge().GetValue()).Should(BeEquivalentTo(0))
				g.Expect(findMetric(metrics, "login_protector_watcher_errors_total", map[string]string{"watcher": "local-session-watcher"})).ShouldNot(BeNil())
			}).WithTimeout(3 * time.Minute).Should(Succeed())
		})
	})
})
