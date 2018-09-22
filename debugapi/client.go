package debugapi

// WIP interface.
// The thread id is exposed, because it's strange if registers can be read or changed without specifying the thread id.
type client interface {
	// LaunchProcess launches the new prcoess.
	// When returned, the process is stopped at the beginning of the program.
	LaunchProcess(name string, arg ...string) (tid int, err error)
	// AttachProcess attaches to the existing process.
	// When returned, the process is stopped.
	AttachProcess(pid int) (tid int, err error)
	DetachProcess()
	ReadMemory()
	WriteMemory()
	ReadRegisters()
	WriteRegisters()
	ReadTLS(offset uint64) (value uint64)
	ContinueAndWait()
	StepAndWait()
}

// EventType represents the type of the event.
type EventType int

const (
	// EventTypeTrapped event happens when the process is trapped.
	EventTypeTrapped EventType = iota
	// EventTypeCoreDump event happens when the process terminates unexpectedly.
	EventTypeCoreDump
	// EventTypeExited event happens when the process exits.
	EventTypeExited
	// EventTypeTerminated event happens when the process is terminated by a signal.
	EventTypeTerminated
)

// IsExitEvent returns true if the event indicates the process exits for some reason.
func IsExitEvent(event EventType) bool {
	return event == EventTypeCoreDump || event == EventTypeExited || event == EventTypeTerminated
}

// Event describes the event happens to the target process.
type Event struct {
	Type EventType
	Data int
}

// Registers represents the target's registers.
type Registers struct {
	Rip uint64
	Rsp uint64
	Rcx uint64
}
