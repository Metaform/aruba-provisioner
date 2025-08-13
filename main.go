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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	yamlString := participantYaml
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_NAME}", *participantName, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_NAME", *participantName, -1)
	yamlString = strings.Replace(yamlString, "${PARTICIPANT_ID}", *did, -1)
	yamlString = strings.Replace(yamlString, "$PARTICIPANT_ID", *did, -1)

	docs := strings.Split(yamlString, "---")

	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			panic(err)
		}

		fmt.Printf("Document %d: kind=%v, name=%v\n",
			i+1,
			obj.GetKind(),
			obj.GetName())

		err = apply(c, ctx, obj)
		if err != nil {
			log.Fatalf("applying yaml: %v", err)
		}
	}

	//var ns = &corev1.Namespace{
	//	TypeMeta: metav1.TypeMeta{
	//		APIVersion: "v1",
	//		Kind:       "Namespace",
	//	},
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name: namespace,
	//	},
	//}
	//err = apply(c, ctx, ns)
	//if err != nil {
	//	log.Fatalf("apply namespace: %v", err)
	//	return
	//}
	//
	//// create config maps
	//createdConfigMap := createConnectorConfig(participantName, namespace, did)
	//err = apply(c, ctx, createdConfigMap)
	//if err != nil {
	//	log.Fatalf("apply connector config map: %v", err)
	//	return
	//}
	//
	//participantsConfig := createParticipantsConfig(participantName, namespace)
	//err = apply(c, ctx, participantsConfig)
	//if err != nil {
	//	log.Fatalf("apply participants config map: %v", err)
	//	return
	//}
	//
	//// create connector control plane deployment
	//createdControlPlane := createControlPlane(participantName, namespace)
	//err = apply(c, ctx, createdControlPlane)
	//if err != nil {
	//	log.Fatalf("apply control plane: %v", err)
	//	return
	//}
	//
	//// Create Ingress object
	//cpIngress := createIngress(participantName+"-ingress", namespace)
	//err = apply(c, ctx, cpIngress)
	//if err != nil {
	//	log.Fatalf("apply connector ingress: %v", err)
	//	return
	//}

}

func createConnectorConfig(participantName string, namespace string, did string) *corev1.ConfigMap {
	issuerDid := "did:web:issuer.mvd-issuer.svc.cluster.local%3A7083"
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      participantName + "-controlplane-config",
			Namespace: namespace,
		},
		Data: map[string]string{
			"EDC_PARTICIPANT_ID":        did,
			"EDC_IAM_ISSUER_ID":         issuerDid,
			"EDC_IAM_DID_WEB_USE_HTTPS": "false",

			"WEB_HTTP_PORT":                 "8080",
			"WEB_HTTP_PATH":                 "/api",
			"WEB_HTTP_MANAGEMENT_PORT":      "8081",
			"WEB_HTTP_MANAGEMENT_PATH":      "/api/management",
			"WEB_HTTP_MANAGEMENT_AUTH_TYPE": "tokenbased",
			"WEB_HTTP_MANAGEMENT_AUTH_KEY":  "password",
			"WEB_HTTP_CONTROL_PORT":         "8083",
			"WEB_HTTP_CONTROL_PATH":         "/api/control",
			"WEB_HTTP_PROTOCOL_PORT":        "8082",
			"WEB_HTTP_PROTOCOL_PATH":        "/api/dsp",
			"WEB_HTTP_CATALOG_PORT":         "8084",
			"WEB_HTTP_CATALOG_PATH":         "/api/catalog",
			"WEB_HTTP_CATALOG_AUTH_TYPE":    "tokenbased",
			"WEB_HTTP_CATALOG_AUTH_KEY":     "password",

			"EDC_DSP_CALLBACK_ADDRESS":      "http://" + participantName + "-controlplane." + namespace + ".svc.cluster.local:8082/api/dsp",
			"EDC_IAM_STS_PRIVATEKEY_ALIAS":  did + "#key-1",
			"EDC_IAM_STS_PUBLICKEY_ID":      did + "#key-1",
			"JAVA_TOOL_OPTIONS":             "-agentlib:jdwp=transport=dt_socket,server=y,suspend=n,address=1044",
			"EDC_IH_AUDIENCE_REGISTRY_PATH": "/etc/registry/registry.json",

			"EDC_VAULT_HASHICORP_URL":   "http://" + participantName + "-vault." + namespace + ".svc.cluster.local:8200",
			"EDC_VAULT_HASHICORP_TOKEN": "root",

			"EDC_MVD_PARTICIPANTS_LIST_FILE": "/etc/participants/participants.json",

			"EDC_DATASOURCE_DEFAULT_URL":      "jdbc:postgresql://" + participantName + "-postgres-service." + namespace + ".svc.cluster.local:5432/" + participantName,
			"EDC_DATASOURCE_DEFAULT_USER":     participantName,
			"EDC_DATASOURCE_DEFAULT_PASSWORD": participantName,
			"EDC_SQL_SCHEMA_AUTOCREATE":       "true",

			"EDC_CATALOG_CACHE_EXECUTION_DELAY_SECONDS":  "10",
			"EDC_CATALOG_CACHE_EXECUTION_PERIOD_SECONDS": "10",

			"EDC_IAM_STS_OAUTH_TOKEN_URL":           "http://foobar/token",
			"EDC_IAM_STS_OAUTH_CLIENT_ID":           did,
			"EDC_IAM_STS_OAUTH_CLIENT_SECRET_ALIAS": did + "-sts-client-secret",
		},
	}
	return configMap
}

func createParticipantsConfig(participantName string, namespace string) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      participantName + "-participants",
			Namespace: namespace,
		},
	}
	return configMap
}

func createControlPlane(participantName string, namespace string) *appsv1.Deployment {
	replicas := int32(1)
	appName := participantName + "-controlplane"
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": appName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": appName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": appName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            appName,
							Image:           "ghcr.io/paullatzelsperger/minimumviabledataspace/controlplane:latest",
							ImagePullPolicy: "Always",
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: participantName + "-controlplane-config",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Name:          "http",
								},
								{
									ContainerPort: 8081,
									Name:          "management",
								},
								{
									ContainerPort: 1044,
									Name:          "debug",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/check/liveness",
										Port: intstr.FromInt32(8080),
									},
								},
								FailureThreshold: 10,
								PeriodSeconds:    5,
								TimeoutSeconds:   30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/check/readiness",
										Port: intstr.FromInt32(8080),
									},
								},
								FailureThreshold: 10,
								PeriodSeconds:    5,
								TimeoutSeconds:   30,
							},

							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/check/startup",
										Port: intstr.FromInt32(8080),
									},
								},
								FailureThreshold: 10,
								PeriodSeconds:    5,
								TimeoutSeconds:   30,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "participants-volume",
									MountPath: "/etc/participants",
								},
								{
									Name:      "config-volume",
									MountPath: "/etc/config",
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "participants-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: participantName + "-participants",
									},
								},
							},
						},
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: appName + "-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return deployment
}

func createIngress(ingressName string, namespace string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypeImplementationSpecific
	ingressClassName := "nginx"
	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: namespace,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/consumer/health(/|$)(.*)",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "consumer-controlplane",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
								{
									Path:     "/consumer/cp(/|$)(.*)",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "consumer-controlplane",
											Port: networkingv1.ServiceBackendPort{
												Number: 8081,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return ingress
}

func apply(c client.Client, ctx context.Context, object client.Object) error {
	// Server-Side Apply
	err := c.Patch(
		ctx,
		object,
		client.Apply,
		client.FieldOwner("my-go-app"),
		// Optional: take ownership of fields (overwrites conflicts)
		// client.ForceOwnership,
	)
	return err
}
