package domain

import (
	"testing"
)

func TestMissionStateMachine_AcceptsValidTransitions(t *testing.T) {
	sm := MissionStateMachine()
	tests := []struct {
		from, to MissionStatus
	}{
		{DeclStatusSubmitted, DeclStatusReviewing},
		{DeclStatusSubmitted, DeclStatusAccepted},
		{DeclStatusSubmitted, DeclStatusQueued},
		{DeclStatusSubmitted, DeclStatusCancelled},
		{DeclStatusReviewing, DeclStatusAccepted},
		{DeclStatusReviewing, DeclStatusRejected},
		{DeclStatusAccepted, DeclStatusScheduled},
		{DeclStatusQueued, DeclStatusScheduled},
		{DeclStatusScheduled, DeclStatusProcessing},
		{DeclStatusProcessing, DeclStatusCompleted},
	}
	for _, tt := range tests {
		if !sm.CanTransition(string(tt.from), string(tt.to)) {
			t.Errorf("expected transition %s -> %s to be allowed", tt.from, tt.to)
		}
	}
}

func TestMissionStateMachine_RejectsInvalidTransitions(t *testing.T) {
	sm := MissionStateMachine()
	tests := []struct {
		from, to MissionStatus
	}{
		{DeclStatusCompleted, DeclStatusSubmitted},
		{DeclStatusRejected, DeclStatusAccepted},
		{DeclStatusCancelled, DeclStatusProcessing},
		{DeclStatusCompleted, DeclStatusCancelled},
		{DeclStatusRejected, DeclStatusCancelled},
	}
	for _, tt := range tests {
		_, err := sm.MustTransition(string(tt.from), string(tt.to))
		if err == nil {
			t.Errorf("expected illegal transition %s -> %s to be rejected", tt.from, tt.to)
		}
	}
}

func TestReservationStateMachine_AllLegalTransitions(t *testing.T) {
	sm := ReservationStateMachine()
	tests := []struct {
		from, to ReservationStatus
	}{
		{ReservationStatusAllocated, ReservationStatusEffective},
		{ReservationStatusAllocated, ReservationStatusCancelled},
		{ReservationStatusAllocated, ReservationStatusEscalated},
		{ReservationStatusEffective, ReservationStatusOccupied},
		{ReservationStatusEffective, ReservationStatusReleased},
		{ReservationStatusEffective, ReservationStatusEscalated},
		{ReservationStatusOccupied, ReservationStatusReleased},
		{ReservationStatusEscalated, ReservationStatusEffective},
	}
	for _, tt := range tests {
		if !sm.CanTransition(string(tt.from), string(tt.to)) {
			t.Errorf("expected transition %s -> %s to be allowed", tt.from, tt.to)
		}
	}
}

func TestReservationStateMachine_IllegalTransitionsRejected(t *testing.T) {
	sm := ReservationStateMachine()
	illegal := []struct {
		from, to ReservationStatus
	}{
		{ReservationStatusReleased, ReservationStatusOccupied},
		{ReservationStatusCancelled, ReservationStatusEffective},
		{ReservationStatusReleased, ReservationStatusCancelled},
	}
	for _, tt := range illegal {
		_, err := sm.MustTransition(string(tt.from), string(tt.to))
		if err == nil {
			t.Errorf("expected illegal transition %s -> %s to be rejected", tt.from, tt.to)
		}
	}
}

func TestMissionRunStateMachine_ValidAndInvalidTransitions(t *testing.T) {
	sm := MissionRunStateMachine()
	if !sm.CanTransition(string(RunStatusCreated), string(RunStatusAssigned)) {
		t.Error("created -> assigned should be allowed")
	}
	if !sm.CanTransition(string(RunStatusInProgress), string(RunStatusCompleted)) {
		t.Error("in_progress -> completed should be allowed")
	}
	_, err := sm.MustTransition(string(RunStatusCreated), string(RunStatusCompleted))
	if err == nil {
		t.Error("created -> completed should be illegal")
	}
	_, err = sm.MustTransition(string(RunStatusCompleted), string(RunStatusCancelled))
	if err == nil {
		t.Error("completed -> cancelled should be illegal")
	}
}

func TestAssignmentStateMachine_PreemptedCanReassign(t *testing.T) {
	sm := AssignmentStateMachine()
	if !sm.CanTransition(string(AssignmentStatusPreempted), string(AssignmentStatusAssigned)) {
		t.Error("preempted -> assigned should be allowed for reassignment")
	}
	if !sm.CanTransition(string(AssignmentStatusAssigned), string(AssignmentStatusClaimed)) {
		t.Error("assigned -> claimed should be allowed")
	}
	_, err := sm.MustTransition(string(AssignmentStatusCreated), string(AssignmentStatusClaimed))
	if err == nil {
		t.Error("created -> claimed should be illegal (must assign first)")
	}
}

func TestQuotaStateMachine_Transitions(t *testing.T) {
	sm := QuotaStateMachine()
	if !sm.CanTransition(string(QuotaStatusAvailable), string(QuotaStatusWarning)) {
		t.Error("available -> warning should be allowed")
	}
	if !sm.CanTransition(string(QuotaStatusExhausted), string(QuotaStatusAvailable)) {
		t.Error("exhausted -> available should be allowed on new day")
	}
	_, err := sm.MustTransition(string(QuotaStatusReserved), string(QuotaStatusCompleted))
	if err == nil {
		t.Error("reserved -> completed should be illegal")
	}
}

func TestStateMachine_AllowedTargets(t *testing.T) {
	sm := MissionStateMachine()
	targets := sm.AllowedTargets(string(DeclStatusSubmitted))
	if len(targets) == 0 {
		t.Error("submitted should have allowed targets")
	}
	found := false
	for _, tgt := range targets {
		if tgt == string(DeclStatusAccepted) {
			found = true
		}
	}
	if !found {
		t.Error("submitted should allow transition to accepted")
	}
}

func TestStateMachine_InitialState(t *testing.T) {
	tests := []struct {
		name     string
		sm       *StateMachine
		expected string
	}{
		{"mission", MissionStateMachine(), string(DeclStatusSubmitted)},
		{"window", ReservationStateMachine(), string(ReservationStatusAllocated)},
		{"mission_run", MissionRunStateMachine(), string(RunStatusCreated)},
		{"robot_assignment", AssignmentStateMachine(), string(AssignmentStatusCreated)},
		{"quota", QuotaStateMachine(), string(QuotaStatusAvailable)},
	}
	for _, tt := range tests {
		if tt.sm.InitialState() != tt.expected {
			t.Errorf("%s initial state: expected %s, got %s", tt.name, tt.expected, tt.sm.InitialState())
		}
	}
}

func TestStateMachine_AllTransitionsNonEmpty(t *testing.T) {
	sms := []*StateMachine{
		MissionStateMachine(),
		ReservationStateMachine(),
		MissionRunStateMachine(),
		AssignmentStateMachine(),
		QuotaStateMachine(),
	}
	for _, sm := range sms {
		transitions := sm.AllTransitions()
		if len(transitions) < 3 {
			t.Errorf("state machine %s has too few transitions: %d", sm.name, len(transitions))
		}
	}
}
