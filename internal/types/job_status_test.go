// SPDX-License-Identifier: MIT

package types

import (
	"encoding/json"
	"testing"
)

func TestJobStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   string
	}{
		{"pending", JobStatusPending, "pending"},
		{"running", JobStatusRunning, "running"},
		{"completed", JobStatusCompleted, "completed"},
		{"failed", JobStatusFailed, "failed"},
		{"cancelled", JobStatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("JobStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"pending valid", JobStatusPending, true},
		{"running valid", JobStatusRunning, true},
		{"completed valid", JobStatusCompleted, true},
		{"failed valid", JobStatusFailed, true},
		{"cancelled valid", JobStatusCancelled, true},
		{"invalid empty", JobStatus(""), false},
		{"invalid unknown", JobStatus("unknown"), false},
		{"invalid typo", JobStatus("runing"), false}, //nolint:misspell // cspell:disable-line
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("JobStatus.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"pending not terminal", JobStatusPending, false},
		{"running not terminal", JobStatusRunning, false},
		{"completed terminal", JobStatusCompleted, true},
		{"failed terminal", JobStatusFailed, true},
		{"cancelled terminal", JobStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("JobStatus.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from JobStatus
		to   JobStatus
		want bool
	}{
		// Valid transitions from Pending
		{"pending to running", JobStatusPending, JobStatusRunning, true},
		{"pending to cancelled", JobStatusPending, JobStatusCancelled, true},
		{"pending to completed", JobStatusPending, JobStatusCompleted, false},
		{"pending to failed", JobStatusPending, JobStatusFailed, false},

		// Valid transitions from Running
		{"running to completed", JobStatusRunning, JobStatusCompleted, true},
		{"running to failed", JobStatusRunning, JobStatusFailed, true},
		{"running to cancelled", JobStatusRunning, JobStatusCancelled, true},
		{"running to pending", JobStatusRunning, JobStatusPending, false},

		// Terminal states cannot transition
		{"completed to running", JobStatusCompleted, JobStatusRunning, false},
		{"failed to running", JobStatusFailed, JobStatusRunning, false},
		{"cancelled to running", JobStatusCancelled, JobStatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Errorf("JobStatus.CanTransitionTo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobStatus_MarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		status  JobStatus
		want    string
		wantErr bool
	}{
		{"pending", JobStatusPending, `"pending"`, false},
		{"running", JobStatusRunning, `"running"`, false},
		{"completed", JobStatusCompleted, `"completed"`, false},
		{"failed", JobStatusFailed, `"failed"`, false},
		{"cancelled", JobStatusCancelled, `"cancelled"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("JobStatus.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf("JobStatus.MarshalJSON() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestJobStatus_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    JobStatus
		wantErr bool
	}{
		{"pending", `"pending"`, JobStatusPending, false},
		{"running", `"running"`, JobStatusRunning, false},
		{"completed", `"completed"`, JobStatusCompleted, false},
		{"failed", `"failed"`, JobStatusFailed, false},
		{"cancelled", `"cancelled"`, JobStatusCancelled, false},
		{"invalid", `"unknown"`, "", true},
		{"malformed", `123`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got JobStatus
			err := json.Unmarshal([]byte(tt.json), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("JobStatus.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("JobStatus.UnmarshalJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJobStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    JobStatus
		wantErr bool
	}{
		{"pending", "pending", JobStatusPending, false},
		{"running", "running", JobStatusRunning, false},
		{"completed", "completed", JobStatusCompleted, false},
		{"failed", "failed", JobStatusFailed, false},
		{"cancelled", "cancelled", JobStatusCancelled, false},
		{"invalid empty", "", "", true},
		{"invalid unknown", "unknown", "", true},
		{"invalid typo", "runing", "", true}, //nolint:misspell // cspell:disable-line
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJobStatus(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseJobStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseJobStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllJobStatuses(t *testing.T) {
	statuses := AllJobStatuses()

	if len(statuses) != 5 {
		t.Errorf("AllJobStatuses() returned %d statuses, want 5", len(statuses))
	}

	// Verify all expected statuses are present
	expected := []JobStatus{
		JobStatusPending,
		JobStatusRunning,
		JobStatusCompleted,
		JobStatusFailed,
		JobStatusCancelled,
	}

	for _, exp := range expected {
		found := false
		for _, s := range statuses {
			if s == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllJobStatuses() missing %v", exp)
		}
	}
}

func TestJobStatus_JSONRoundTrip(t *testing.T) {
	original := JobStatusRunning

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal back
	var decoded JobStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify round-trip
	if decoded != original {
		t.Errorf("Round-trip failed: got %v, want %v", decoded, original)
	}
}
