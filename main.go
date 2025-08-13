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

const readinessPollInterval = 2 * time.Second

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
			waitForDeploymentsAsync(
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

// waitForDeploymentsAsync runs the readiness check in the background and invokes the callback on success.
func waitForDeploymentsAsync(
	c client.Client,
	ctx context.Context,
	namespace string,
	deployments []string,
	callback func(),
) {
	fmt.Println("Waiting for deployments", deployments, "")
	go func() {
		if err := waitForDeployments(c, ctx, namespace, deployments); err != nil {
			fmt.Printf("deployment readiness check failed for namespace %s: %v\n", namespace, err)
			return
		}
		callback()
	}()
}

// waitForDeployments waits for all given deployments concurrently and returns an error if any fail.
func waitForDeployments(c client.Client, ctx context.Context, namespace string, deployments []string) error {
	errCh := make(chan error, len(deployments))
	for _, name := range deployments {
		name := name // capture
		go func() {
			errCh <- waitForDeployment(c, ctx, namespace, name)
		}()
	}
	var firstErr error
	for _, deployment := range deployments {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		} else if err == nil {
			fmt.Println("Deployment", deployment, "ready")
		}
	}
	return firstErr
}

// waitForDeployment polls until the deployment reaches the desired ready replicas.
func waitForDeployment(c client.Client, ctx context.Context, namespace string, name string) error {
	deployment := &appsv1.Deployment{}
	for {
		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, deployment); err != nil {
			return err
		}

		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Status.ReadyReplicas == desired {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessPollInterval):
			continue
		}
	}
}
