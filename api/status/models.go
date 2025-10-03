package status

import "time"

type ProvisioningStatus string

const (
	StatusProvisioning ProvisioningStatus = "PROVISIONING"
	StatusReady        ProvisioningStatus = "READY"
	StatusDegraded     ProvisioningStatus = "DEGRADED"
	StatusFailed       ProvisioningStatus = "FAILED"
	StatusDeleting     ProvisioningStatus = "DELETING"
	StatusNotFound     ProvisioningStatus = "NOT_FOUND"
)

type ComponentStatus struct {
	Status   string        `json:"status"`
	Ready    bool          `json:"ready"`
	Replicas ReplicaStatus `json:"replicas"`
	Message  string        `json:"message,omitempty"`
}

type ReplicaStatus struct {
	Desired int32 `json:"desired"`
	Current int32 `json:"current"`
	Ready   int32 `json:"ready"`
}

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
}

// Full response for GET /resources/{name}/status
type ParticipantStatusResponse struct {
	ParticipantName string                     `json:"participantName"`
	Status          ProvisioningStatus         `json:"status"`
	LastUpdated     time.Time                  `json:"lastUpdated"`
	Components      map[string]ComponentStatus `json:"components"`
	Message         string                     `json:"message"`
	Events          []Event                    `json:"events,omitempty"`
}

type ParticipantSummary struct {
	ParticipantName string             `json:"participantName"`
	Status          ProvisioningStatus `json:"status"`
	LastUpdated     time.Time          `json:"lastUpdated"`
}
