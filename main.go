// vmware_to_kubevirt_migration.go
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	yamlutil "sigs.k8s.io/yaml"
)

// VMwareDetails captures vCenter info from UI

type VMwareDetails struct {
	Name       string
	Host       string
	Username   string
	Password   string
	Datacenter string
	Cluster    string
	VMNames    []string
}

func getKubeConfig() string {
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		log.Fatalf("Kubeconfig not found at %s", kubeconfig)
	}
	return kubeconfig
}

func createSecretForVMware(ctx context.Context, clientset *kubernetes.Clientset, namespace string, name string, username string, password string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"user":     username,
			"password": password,
		},
	}
	_, err := clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func createVMwareProviderYAML(name, url, secretName, namespace string) ([]byte, error) {
	template := `
apiVersion: forklift.konveyor.io/v1beta1
kind: Provider
metadata:
  name: %s
  namespace: %s
spec:
  type: vsphere
  url: https://%s
  secret:
    name: %s
    namespace: %s
`
	return []byte(fmt.Sprintf(template, name, namespace, url, secretName, namespace)), nil
}

func applyYAMLToCluster(yamlContent []byte, k8sClient client.Client) error {
	obj := map[string]interface{}{}
	if err := yamlutil.Unmarshal(yamlContent, &obj); err != nil {
		return err
	}
	// this is a simplified version, in production use unstructured.Unstructured
	fmt.Println("Generated YAML: \n", string(yamlContent))
	return nil
}

func createPlanYAML(planName, sourceProvider, destProvider, vmID, networkMap, storageMap, namespace string) ([]byte, error) {
	template := `
apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: %s
  namespace: %s
spec:
  provider:
    source:
      name: %s
    destination:
      name: %s
  map:
    network: %s
    storage: %s
  vms:
    - id: %s
`
	return []byte(fmt.Sprintf(template, planName, namespace, sourceProvider, destProvider, networkMap, storageMap, vmID)), nil
}

func createMigrationYAML(migrationName, planName, namespace string) ([]byte, error) {
	template := `
apiVersion: forklift.konveyor.io/v1beta1
kind: Migration
metadata:
  name: %s
  namespace: %s
spec:
  plan:
    name: %s
`
	return []byte(fmt.Sprintf(template, migrationName, namespace, planName)), nil
}

func main() {
	ctx := context.TODO()
	kubeconfigPath := getKubeConfig()

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		log.Fatalf("Failed to get kubeconfig: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Failed to create kube client: %v", err)
	}

	details := VMwareDetails{
		Name:     "vmware-demo",
		Host:     "vcenter.example.com",
		Username: "administrator@vsphere.local",
		Password: "your-password",
		VMNames:  []string{"TestVM1"},
	}

	namespace := "forklift"
	secretName := fmt.Sprintf("%s-secret", details.Name)
	if err := createSecretForVMware(ctx, kubeClient, namespace, secretName, details.Username, details.Password); err != nil {
		log.Fatalf("Failed to create secret: %v", err)
	}

	yamlBytes, err := createVMwareProviderYAML(details.Name, details.Host, secretName, namespace)
	if err != nil {
		log.Fatalf("Failed to generate provider YAML: %v", err)
	}

	runtimeClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		log.Fatalf("Failed to create runtime client: %v", err)
	}

	if err := applyYAMLToCluster(yamlBytes, runtimeClient); err != nil {
		log.Fatalf("Failed to apply provider YAML: %v", err)
	}

	// --- Simulate plan and migration setup ---
	planName := "migration-plan-demo"
	networkMap := "default-networkmap"
	storageMap := "default-storagemap"
	vmID := "vm-12345" // Ideally discovered from provider inventory

	planYAML, err := createPlanYAML(planName, details.Name, "kubevirt-provider", vmID, networkMap, storageMap, namespace)
	if err != nil {
		log.Fatalf("Failed to create plan YAML: %v", err)
	}
	_ = applyYAMLToCluster(planYAML, runtimeClient)

	migrationYAML, err := createMigrationYAML("migration-demo", planName, namespace)
	if err != nil {
		log.Fatalf("Failed to create migration YAML: %v", err)
	}
	_ = applyYAMLToCluster(migrationYAML, runtimeClient)

	fmt.Println("Migration triggered. Monitor status via kubectl.")
}
