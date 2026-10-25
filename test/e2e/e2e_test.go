//go:build e2e_ebpf

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	ghostkeyNamespace = "ghostkey-system"
	testNamespace     = "ghostkey-e2e"
	defaultTimeout    = 120 * time.Second
	pollInterval      = 2 * time.Second
)

var (
	clientset     *kubernetes.Clientset
	repoRoot      string
	chartDir      string // path to the Helm chart directory
	imageRegistry string // E2E_REGISTRY env var: if set, images are prefixed (e.g. "ttl.sh/ghostkey-abc123")
)

// findRepoRoot returns the absolute path to the repository root
// by looking for go.mod starting from the test directory.
func findRepoRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to find module root: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func TestMain(m *testing.M) {
	imageRegistry = os.Getenv("E2E_REGISTRY")

	var err error
	repoRoot, err = findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find repo root: %v\n", err)
		os.Exit(1)
	}

	// Build kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kubeconfig: %v\n", err)
		os.Exit(1)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create clientset: %v\n", err)
		os.Exit(1)
	}

	// Deploy GhostKey using Helm chart (skip if E2E_SKIP_INSTALL is set)
	skipInstall := os.Getenv("E2E_SKIP_INSTALL") != ""
	if skipInstall {
		fmt.Println("Skipping Helm install (E2E_SKIP_INSTALL set, assuming ghostkey is already deployed)")
	} else {
		chartDir = filepath.Join(repoRoot, "charts", "ghostkey")
		valuesFile := filepath.Join(repoRoot, "test", "e2e", "values-e2e.yaml")
		helmArgs := []string{"install", "ghostkey", chartDir, "-n", ghostkeyNamespace, "--create-namespace", "-f", valuesFile, "--wait", "--timeout", "120s"}
		if imageRegistry != "" {
			helmArgs = append(helmArgs,
				"--set", "image.repository="+imageRegistry+"/ghostkey",
				"--set", "image.pullPolicy=Always",
			)
		}
		// E2E_HELM_SET adds comma-separated key=value overrides, e.g.
		// "controller.ebpf.tcAttachMode=clsact" to run the suite against the
		// clsact tc fallback on a TCX-capable kernel.
		for _, kv := range strings.Split(os.Getenv("E2E_HELM_SET"), ",") {
			if kv = strings.TrimSpace(kv); kv != "" {
				helmArgs = append(helmArgs, "--set", kv)
			}
		}
		if _, err := helm(helmArgs...); err != nil {
			fmt.Fprintf(os.Stderr, "failed to deploy ghostkey: %v\n", err)
			os.Exit(1)
		}
	}

	// Wait for controller and webhook to be ready
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	fmt.Println("Waiting for ghostkey-controller DaemonSet...")
	if err := waitForDaemonSetReady(ctx, ghostkeyNamespace, "ghostkey-controller"); err != nil {
		fmt.Fprintf(os.Stderr, "controller not ready: %v\n", err)
		dumpLogs()
		teardown()
		os.Exit(1)
	}

	fmt.Println("Waiting for ghostkey-webhook Deployment...")
	if err := waitForDeploymentReady(ctx, ghostkeyNamespace, "ghostkey-webhook"); err != nil {
		fmt.Fprintf(os.Stderr, "webhook not ready: %v\n", err)
		dumpLogs()
		teardown()
		os.Exit(1)
	}

	// Create test namespace with ghostkey.io/enabled=true
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
			Labels: map[string]string{
				"ghostkey.io/enabled": "true",
			},
		},
	}
	if _, err := clientset.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create test namespace: %v\n", err)
		teardown()
		os.Exit(1)
	}

	fmt.Println("GhostKey deployed and ready. Running tests...")
	code := m.Run()

	teardown()
	os.Exit(code)
}

func teardown() {
	// Delete test namespace (cascade deletes all resources in it)
	_ = clientset.CoreV1().Namespaces().Delete(context.Background(), testNamespace, metav1.DeleteOptions{})
	// Remove ghostkey deployment unless we skipped install.
	// --wait blocks until the controller and webhook pods are deleted, so the
	// Go coverage runtime has time to flush covcounters to the hostPath
	// volume on clean SIGTERM. Without it, helm returns immediately and the
	// next CI step races kubelet's SIGKILL.
	if os.Getenv("E2E_SKIP_INSTALL") == "" {
		_, _ = helm("uninstall", "ghostkey", "-n", ghostkeyNamespace, "--wait", "--timeout", "90s")
	}
}

func dumpLogs() {
	fmt.Println("=== GhostKey Controller Logs (previous) ===")
	out, _ := kubectl("logs", "-n", ghostkeyNamespace, "-l", "app.kubernetes.io/component=controller", "--tail=50", "--previous")
	fmt.Println(out)
	fmt.Println("=== GhostKey Controller Logs ===")
	out, _ = kubectl("logs", "-n", ghostkeyNamespace, "-l", "app.kubernetes.io/component=controller", "--tail=50")
	fmt.Println(out)
	fmt.Println("=== GhostKey Webhook Logs ===")
	out, _ = kubectl("logs", "-n", ghostkeyNamespace, "-l", "app.kubernetes.io/component=webhook", "--tail=50")
	fmt.Println(out)
	fmt.Println("=== All Pods ===")
	out, _ = kubectl("get", "pods", "-A")
	fmt.Println(out)
}
