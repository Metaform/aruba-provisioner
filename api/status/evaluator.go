package status

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatusEvaluator struct{}

func NewStatusEvaluator() *StatusEvaluator {
	return &StatusEvaluator{}
}

// To be considered READY. These are the core components required for basic functionality.
//
// Components:
//   - controlplane: EDC Control Plane
//   - dataplane:    EDC Data Plane
//   - identityhub:  Identity Hub
//   - postgres:     PostgreSQL
//
// This list is currently hardcoded but could be made configurable via env. or config file in future versions (if flexibility is needed).
var criticalDeployments = []string{"controlplane", "dataplane", "identityhub", "postgres"}

func (se *StatusEvaluator) GetDeploymentStatus(deployment *appsv1.Deployment) ComponentStatus {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	current := deployment.Status.Replicas
	ready := deployment.Status.ReadyReplicas

	status := "Unknown"
	isReady := false
	message := ""

	if ready == desired && desired > 0 {
		status = "Running"
		isReady = true
	} else if current == 0 {
		status = "Pending"
		message = "No pods are running"
	} else if ready < desired {
		status = "Starting"
		message = fmt.Sprintf("%d of %d replicas ready", ready, desired)
	} else if deployment.Status.UnavailableReplicas > 0 {
		status = "Degraded"
		message = fmt.Sprintf("%d replicas unavailable", deployment.Status.UnavailableReplicas)
	}

	return ComponentStatus{
		Status: status,
		Ready:  isReady,
		Replicas: ReplicaStatus{
			Desired: desired,
			Current: current,
			Ready:   ready,
		},
		Message: message,
	}
}

func (se *StatusEvaluator) GetStatefulSetStatus(sts *appsv1.StatefulSet) ComponentStatus {
	desired := int32(1)
	if sts.Spec.Replicas != nil {
		desired = *sts.Spec.Replicas
	}

	current := sts.Status.Replicas
	ready := sts.Status.ReadyReplicas

	status := "Unknown"
	isReady := false
	message := ""

	if ready == desired && desired > 0 {
		status = "Running"
		isReady = true
	} else if current == 0 {
		status = "Pending"
		message = "No pods are running"
	} else if ready < desired {
		status = "Starting"
		message = fmt.Sprintf("%d of %d replicas ready", ready, desired)
	}

	return ComponentStatus{
		Status: status,
		Ready:  isReady,
		Replicas: ReplicaStatus{
			Desired: desired,
			Current: current,
			Ready:   ready,
		},
		Message: message,
	}
}

func (se *StatusEvaluator) DetermineOverallStatus(components map[string]ComponentStatus) (ProvisioningStatus, string) {
	if len(components) == 0 {
		return StatusProvisioning, "No components found, provisioning may be in progress"
	}

	allCriticalReady := true
	anyNonCriticalNotReady := false
	criticalNotReadyCount := 0
	messages := []string{}

	for _, deploymentName := range criticalDeployments {
		component, exists := components[deploymentName]
		if !exists {
			allCriticalReady = false
			criticalNotReadyCount++
			messages = append(messages, fmt.Sprintf("Critical component %s not found", deploymentName))
			continue
		}

		if !component.Ready {
			allCriticalReady = false
			criticalNotReadyCount++
			if component.Message != "" {
				messages = append(messages, fmt.Sprintf("%s: %s", deploymentName, component.Message))
			}
		}
	}

	// Check non-critical components
	for name, component := range components {
		isCritical := false
		for _, critical := range criticalDeployments {
			if name == critical {
				isCritical = true
				break
			}
		}
		if !isCritical && !component.Ready {
			anyNonCriticalNotReady = true
		}
	}

	if allCriticalReady && !anyNonCriticalNotReady {
		return StatusReady, "All components are running and ready"
	} else if allCriticalReady && anyNonCriticalNotReady {
		return StatusDegraded, "All critical components ready, but some non-critical components are not ready"
	} else if criticalNotReadyCount == len(criticalDeployments) {
		// All critical components missing/not ready - likely still provisioning
		return StatusProvisioning, "Critical components are not yet ready"
	} else {
		// Some critical components ready, some not - degraded state
		msg := fmt.Sprintf("%d of %d critical components not ready", criticalNotReadyCount, len(criticalDeployments))
		if len(messages) > 0 {
			msg = msg + ": " + messages[0] // Include first issue
		}
		return StatusDegraded, msg
	}
}

func (se *StatusEvaluator) GetRecentEvents(ctx context.Context, kubeClient client.Client, namespace string) ([]Event, error) {
	eventList := &corev1.EventList{}
	err := kubeClient.List(ctx, eventList, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	// Filter events from last 30 minutes
	events := make([]Event, 0)
	cutoff := time.Now().Add(-30 * time.Minute)

	for _, event := range eventList.Items {
		if event.LastTimestamp.Time.After(cutoff) {
			events = append(events, Event{
				Timestamp: event.LastTimestamp.Time,
				Type:      event.Type,
				Message:   event.Message,
			})
		}
	}

	// Sort by timestamp (most recent first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	if len(events) > 10 {
		events = events[:10]
	}

	return events, nil
}

func IsSystemNamespace(name string) bool {
	systemNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}

	for _, sysNs := range systemNamespaces {
		if name == sysNs {
			return true
		}
	}

	return false
}

// checks if an error is due to Kubernetes API being unavailable.
func IsKubernetesUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Common Kubernetes connectivity error patterns
	unavailablePatterns := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"timeout",
		"timed out",
		"unable to connect",
		"dial tcp",
		"i/o timeout",
		"context deadline exceeded",
		"server is currently unable",
		"TLS handshake",
		"network is unreachable",
		"EOF",
	}

	for _, pattern := range unavailablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}
