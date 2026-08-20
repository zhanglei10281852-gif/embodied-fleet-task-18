package mission

import (
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func auditNew(st *store.SQLiteStore, clock apperr.Clock) *audit.Recorder {
	return audit.New(st, clock)
}

// NewMission creates a valid mission submit request for testing.
func NewMission(robotName string) SubmitRequest {
	return SubmitRequest{
		RobotName:            robotName,
		RobotSerial:          "IMO" + robotName,
		FleetCode:            "V001",
		PlannedStartAt:       time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		MissionKind:          "inspection",
		EstimatedEnergyUnits: 10,
		ChargeMode:           "standard",
		RequestedBy:          "agent-1",
		RequestingParty:      "operations_dispatch",
		Priority:             5,
		IdempotencyKey:       "idem-" + uuid.NewString(),
	}
}
