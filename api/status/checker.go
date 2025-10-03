package status

import (
	"context"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatusChecker struct {
	kubeClient client.Client
	cache      *statusCache
	evaluator  *StatusEvaluator
}

func NewStatusChecker(kubeClient client.Client) *StatusChecker {
	return &StatusChecker{
		kubeClient: kubeClient,
		cache:      newStatusCache(10 * time.Second),
		evaluator:  NewStatusEvaluator(),
	}
}

func (sc *StatusChecker) GetParticipantStatus(ctx context.Context, participantName string) (*ParticipantStatusResponse, error) {
	// If the caller hasn't set a deadline, add a default timeout in order to prevent indefinite blocking on Kubernetes API calls
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Check cache first ( to avoid redundant Kubernetes API calls )
	if cached := sc.cache.get(participantName); cached != nil {
		log.Printf("Cache hit for participant %s", participantName)
		return cached, nil
	}
	log.Printf("Cache miss for participant %s", participantName)

	namespace := &corev1.Namespace{}
	err := sc.kubeClient.Get(ctx, client.ObjectKey{Name: participantName}, namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			response := &ParticipantStatusResponse{
				ParticipantName: participantName,
				Status:          StatusNotFound,
				LastUpdated:     time.Now(),
				Message:         fmt.Sprintf("Namespace %s does not exist", participantName),
				Components:      make(map[string]ComponentStatus),
			}
			// Cache NOT_FOUND responses to avoid repeated K8s API calls
			sc.cache.set(participantName, response)
			return response, nil
		}
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	// Check if namespace is being deleted
	if namespace.DeletionTimestamp != nil {
		response := &ParticipantStatusResponse{
			ParticipantName: participantName,
			Status:          StatusDeleting,
			LastUpdated:     time.Now(),
			Message:         fmt.Sprintf("Namespace %s is being deleted", participantName),
			Components:      make(map[string]ComponentStatus),
		}
		sc.cache.set(participantName, response)
		return response, nil
	}

	// Get component statuses
	components, err := sc.getComponentStatuses(ctx, participantName)
	if err != nil {
		return nil, fmt.Errorf("failed to get component statuses: %w", err)
	}

	overallStatus, message := sc.evaluator.DetermineOverallStatus(components)

	// Get recent events (if errors, just log a warning and continue)
	events, err := sc.evaluator.GetRecentEvents(ctx, sc.kubeClient, participantName)
	if err != nil {
		fmt.Printf("Warning: failed to get events for namespace %s: %v\n", participantName, err)
		events = []Event{}
	}

	response := &ParticipantStatusResponse{
		ParticipantName: participantName,
		Status:          overallStatus,
		LastUpdated:     time.Now(),
		Components:      components,
		Message:         message,
		Events:          events,
	}

	sc.cache.set(participantName, response)

	return response, nil
}

func (sc *StatusChecker) getComponentStatuses(ctx context.Context, namespace string) (map[string]ComponentStatus, error) {
	deploymentList := &appsv1.DeploymentList{}
	err := sc.kubeClient.List(ctx, deploymentList, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	components := make(map[string]ComponentStatus)
	for _, deployment := range deploymentList.Items {
		components[deployment.Name] = sc.evaluator.GetDeploymentStatus(&deployment)
	}

	statefulSetList := &appsv1.StatefulSetList{}
	err = sc.kubeClient.List(ctx, statefulSetList, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulSetList.Items {
		components[sts.Name] = sc.evaluator.GetStatefulSetStatus(&sts)
	}

	return components, nil
}

func (sc *StatusChecker) ListParticipants(ctx context.Context, statusFilter string, page, limit int) ([]ParticipantSummary, int, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	namespaceList := &corev1.NamespaceList{}
	err := sc.kubeClient.List(ctx, namespaceList)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list namespaces: %w", err)
	}

	participants := make([]ParticipantSummary, 0)

	for _, ns := range namespaceList.Items {
		// Skip system namespaces
		if IsSystemNamespace(ns.Name) {
			continue
		}

		hasDeployments, err := sc.hasParticipantDeployments(ctx, ns.Name)
		if err != nil {
			fmt.Printf("Warning: failed to check deployments in namespace %s: %v\n", ns.Name, err)
			continue
		}

		if !hasDeployments {
			continue
		}

		// Get full status (will use in memory cache if available)
		status, err := sc.GetParticipantStatus(ctx, ns.Name)
		if err != nil {
			fmt.Printf("Warning: failed to get status for namespace %s: %v\n", ns.Name, err)
			continue
		}

		if statusFilter != "" && string(status.Status) != statusFilter {
			continue
		}

		participants = append(participants, ParticipantSummary{
			ParticipantName: ns.Name,
			Status:          status.Status,
			LastUpdated:     status.LastUpdated,
		})
	}

	// Apply status filter if provided
	if statusFilter != "" {
		filtered := make([]ParticipantSummary, 0)
		for _, p := range participants {
			if string(p.Status) == statusFilter {
				filtered = append(filtered, p)
			}
		}
		participants = filtered
	}

	total := len(participants)

	// Apply in memory pagination
	start := (page - 1) * limit
	end := start + limit

	if start >= total {
		return []ParticipantSummary{}, total, nil
	}

	if end > total {
		end = total
	}

	return participants[start:end], total, nil
}

// hasParticipantDeployments checks if a namespace has any of our participant deployments
func (sc *StatusChecker) hasParticipantDeployments(ctx context.Context, namespace string) (bool, error) {
	deploymentList := &appsv1.DeploymentList{}
	err := sc.kubeClient.List(ctx, deploymentList, client.InNamespace(namespace))
	if err != nil {
		return false, err
	}

	// Check if has at least one of our provisioner deployments
	for _, deployment := range deploymentList.Items {
		if isProvisionerDeployment(deployment.Name) {
			return true, nil
		}
	}

	return false, nil
}

// isProvisionerDeployment checks if a deployment name is one of our provisioner deployments
func isProvisionerDeployment(name string) bool {
	provisionerDeployments := []string{"controlplane", "dataplane", "identityhub", "postgres"}
	for _, dep := range provisionerDeployments {
		if name == dep {
			return true
		}
	}
	return false
}

func (sc *StatusChecker) InvalidateCache(participantName string) {
	sc.cache.invalidate(participantName)
}

func (sc *StatusChecker) ClearCache() {
	sc.cache.clear()
}

func (sc *StatusChecker) Close() {
	sc.cache.stop()
}
