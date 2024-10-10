package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	. "github.com/onsi/ginkgo/v2" //nolint:golint,revive
	. "github.com/onsi/gomega"
)

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) ([]byte, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		fmt.Fprintf(GinkgoWriter, "chdir dir: %s\n", err)
	}

	command := strings.Join(cmd.Args, " ")
	fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed with error: (%v) %s", command, err, string(output))
	}

	return output, nil
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	re := regexp.MustCompile("/test/.*")
	wd = re.ReplaceAllString(wd, "")
	return wd, nil
}

func Kubectl(input io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = input
	stdout, err := Run(cmd)
	if err == nil {
		return stdout, nil
	}
	return nil, fmt.Errorf("kubectl failed with %s", err)
}

func KubectlSafe(input io.Reader, args ...string) []byte {
	out, err := Kubectl(input, args...)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return out
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func GetResource[T runtime.Object](ns, name string, obj T, opts ...string) error {
	var args []string
	if ns != "" {
		args = append(args, "-n", ns)
	}
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return err
	}
	if strings.HasSuffix(gvk.Kind, "List") && meta.IsListType(obj) {
		gvk.Kind = gvk.Kind[:len(gvk.Kind)-4]
	}
	args = append(args, "get", gvk.Kind)
	if name != "" {
		args = append(args, name)
	}
	args = append(args, "-o", "json")
	args = append(args, opts...)
	data, err := Kubectl(nil, args...)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if meta.IsListType(obj) {
			data = []byte(fmt.Sprintf(`{"apiVersion":"%s","kind":"%s","items":[]}`, gvk.GroupVersion().String(), gvk.Kind))
		} else {
			data = []byte(fmt.Sprintf(`{"apiVersion":"%s","kind":"%s"}`, gvk.GroupVersion().String(), gvk.Kind))
		}
	}
	return json.Unmarshal(data, obj)
}
