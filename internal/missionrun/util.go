package missionrun

import "github.com/google/uuid"

func newUUID() string { return uuid.NewString() }

// id is a no-op string passthrough for readability at call sites.
func id(s string) string { return s }
