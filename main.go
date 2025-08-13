// Go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

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

func main() {
	participantName := flag.String("participant", "", "Name of the participant")
	did := flag.String("did", "", "DID of the participant")
	kubeconfig := flag.String("kubeconfig", "~/.kube/config", "Path to kubeconfig file")
	shouldDelete := flag.Bool("delete", false, "Delete resources")
	flag.Parse()

	if *participantName == "" {
		log.Fatal("--participant is required")
	}
	if *did == "" {
		log.Fatal("--did is required")
	}

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

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	act := apply
	if *shouldDelete {
		fmt.Println("Deleting resources")
		act = func(c client.Client, ctx context.Context, object client.Object) error {
			return c.Delete(ctx, object)
		}
	}
	processYaml(participantName, did, c, ctx, participantYaml, act)
	processYaml(participantName, did, c, ctx, identityhubYaml, act)

	//delete resources
	//time.Sleep(time.Second * 10)
	//processYaml(participantName, did, c, ctx, participantYaml, func(c client.Client, ctx context.Context, object client.Object) error {
	//	return c.Delete(ctx, object)
	//})
	//processYaml(participantName, did, c, ctx, identityhubYaml, func(c client.Client, ctx context.Context, object client.Object) error {
	//	return c.Delete(ctx, object)
	//})
}

type action func(client.Client, context.Context, client.Object) error

func processYaml(participantName *string, did *string, c client.Client, ctx context.Context, yamlString string, kubernetesAction action) {
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_NAME}", *participantName, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_NAME", *participantName, -1)
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_ID}", *did, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_ID", *did, -1)

	docs := strings.Split(yamlString, "---")

	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			panic(err)
		}

		fmt.Printf("Processing resource: kind=%v, name=%v\n",
			obj.GetKind(),
			obj.GetName())

		err := kubernetesAction(c, ctx, obj)
		if err != nil {
			log.Fatalf("applying yaml: %v", err)
		}
	}
}

func apply(c client.Client, ctx context.Context, object client.Object) error {
	// Server-Side Apply
	err := c.Patch(
		ctx,
		object,
		client.Apply,
		client.FieldOwner("my-go-app"),
		// Optional: take ownership of fields (overwrites conflicts)
		client.ForceOwnership,
	)
	return err
}
