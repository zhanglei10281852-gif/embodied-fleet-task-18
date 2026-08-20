package quota

// newUUID generates a unique ID. We use a local copy to keep the package self-contained.
func newUUID() string {
	return uuidString()
}
