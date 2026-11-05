// Package budget holds the machine memory budget shared by hardware detection
// and setup admission. It performs no reads or effects.
package budget

const (
	WiredSourceMetal = "live-metal"

	// OSFloorMiB is the admission allowance kept for macOS and transient
	// allocations. It is policy, not a measurement.
	OSFloorMiB int64 = 1024
)

// Machine contains observed machine memory capacities in MiB.
type Machine struct {
	PhysicalMiB   int64
	DeviceMiB     int64
	WiredLimitMiB int64
	WiredSource   string
}
