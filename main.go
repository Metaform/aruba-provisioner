// Go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	_ "embed"
)

//go:embed connector.yaml
var participantYaml string

//go:embed identityhub.yaml
var identityhubYaml string

// Centralize deployment names used for readiness checks
var participantDeploymentNames = []string{"controlplane", "identityhub", "dataplane"}

func main() {
	kubeconfig := flag.String("kubeconfig", "~/.kube/config", "Path to kubeconfig file")
	flag.Parse()

	ctx := context.Background()

	// Load kubeconfig (or use in-cluster if applicable)
	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("load kubeconfig: %v", err)
	}

	// Scheme with core types
	// --- Prepare scheme ---
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	kubeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	app := fiber.New()
	{
		group := app.Group("/api/v1")
		group.Post("/", func(c *fiber.Ctx) error {
			var request ParticipantDefinition
			if err := c.BodyParser(&request); err != nil {
				return err
			}

			fmt.Println("Creating resources")
			resources1, e1 := applyYaml(&request.ParticipantName, &request.Did, kubeClient, ctx, participantYaml, applyResource)
			if e1 != nil {
				return e1
			}
			resources2, e2 := applyYaml(&request.ParticipantName, &request.Did, kubeClient, ctx, identityhubYaml, applyResource)
			if e2 != nil {
				return e2
			}
			// Merge maps
			mergedResources := make(map[string]string)
			for k, v := range resources1 {
				mergedResources[k] = v
			}
			for k, v := range resources2 {
				mergedResources[k] = v
			}

			// Introduce a clear variable for namespace usage
			namespace := request.ParticipantName

			// Start readiness wait in a separate goroutine (non-blocking request)
			waitForParticipantDeploymentsAsync(
				kubeClient,
				ctx,
				namespace,
				participantDeploymentNames,
				func() {
					fmt.Println("Deployments ready in namespace", namespace, "")
				},
			)

			return c.JSON(mergedResources)

		})
		group.Delete("/", func(c *fiber.Ctx) error {
			var request ParticipantDefinition
			if err := c.BodyParser(&request); err != nil {
				return err
			}
			fmt.Println("Deleting resources")
			resources1, e1 := applyYaml(&request.ParticipantName, &request.Did, kubeClient, ctx, participantYaml, deleteResource)
			if e1 != nil {
				return e1
			}
			resources2, e2 := applyYaml(&request.ParticipantName, &request.Did, kubeClient, ctx, identityhubYaml, deleteResource)
			if e2 != nil {
				return e2
			}
			// Merge maps
			mergedResources := make(map[string]string)
			for k, v := range resources1 {
				mergedResources[k] = v
			}
			for k, v := range resources2 {
				mergedResources[k] = v
			}

			return c.JSON(mergedResources)
		})

	}
	err = app.Listen(":9999")
	if err != nil {
		panic(err)
	}
}

type ParticipantDefinition struct {
	ParticipantName string `json:"participantName,omitempty" validate:"required"`
	Did             string `json:"did,omitempty" validate:"required"`
}

type action func(client.Client, context.Context, client.Object) error

func applyYaml(participantName *string, did *string, c client.Client, ctx context.Context, yamlString string, kubernetesAction action) (map[string]string, error) {
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_NAME}", *participantName, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_NAME", *participantName, -1)
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_ID}", *did, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_ID", *did, -1)

	docs := strings.Split(yamlString, "---")

	resourceMap := make(map[string]string)
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, err
		}

		resourceMap[obj.GetName()] = obj.GetKind()
		err := kubernetesAction(c, ctx, obj)
		if err != nil {
			return nil, err
		}
	}
	return resourceMap, nil
}

func applyResource(c client.Client, ctx context.Context, object client.Object) error {
	// Server-Side Apply
	err := c.Patch(
		ctx,
		object,
		client.Apply,
		client.FieldOwner("go-provisioner"),
		// Optional: take ownership of fields (overwrites conflicts)
		client.ForceOwnership,
	)
	return err
}

func deleteResource(c client.Client, ctx context.Context, object client.Object) error {
	return c.Delete(ctx, object)
}

// Start deployment readiness check in the background and log the outcome. The callback is called when all
// deployments are ready.
func waitForParticipantDeploymentsAsync(
	c client.Client,
	ctx context.Context,
	namespace string,
	deployments []string,
	callback func(),
) {
	fmt.Println("Waiting for deployments", deployments, "")
	go func() {
		if err := waitForMultipleDeployments(c, ctx, namespace, deployments); err != nil {
			fmt.Printf("deployment readiness check failed for namespace %s: %v\n", namespace, err)
			return
		}
		callback()
	}()
}

// Wait for a deployment to be ready by querying the Kubernetes API every 2 seconds and checking whether the
// desired number of replicas is reached.
func waitForDeployment(c client.Client, ctx context.Context, namespace string, name string) error {
	deployment := &appsv1.Deployment{}
	for {
		err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, deployment)
		if err != nil {
			return err
		}

		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second * 2):
			continue
		}
	}
}

// waitForDeploymentAsync starts a goroutine to wait for a deployment to be ready and returns a channel for error reporting.
func waitForDeploymentAsync(c client.Client, ctx context.Context, namespace string, name string) chan error {
	errChan := make(chan error, 1)
	go func() {
		errChan <- waitForDeployment(c, ctx, namespace, name)
	}()
	return errChan
}

// waitForMultipleDeployments waits for multiple deployments in a given namespace to be ready and returns an error if any fail.
func waitForMultipleDeployments(c client.Client, ctx context.Context, namespace string, deployments []string) error {
	errChans := make([]chan error, len(deployments))
	for i, deployment := range deployments {
		errChans[i] = waitForDeploymentAsync(c, ctx, namespace, deployment)
	}

	for i, deployment := range deployments {
		if err := <-errChans[i]; err != nil {
			return fmt.Errorf("waiting for deployment %s: %w", deployment, err)
		} else {
			fmt.Println("Deployment", deployment, "ready")
		}
	}
	return nil
}
