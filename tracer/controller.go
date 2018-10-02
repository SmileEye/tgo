package tracer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/tracee"
)

// ErrInterrupted indicates the tracer is interrupted due to the Interrupt() call.
var ErrInterrupted = errors.New("interrupted")

// Controller controls the associated tracee process.
type Controller struct {
	process     *tracee.Process
	statusStore map[int]goRoutineStatus

	tracingPoint *tracingPoint
	depth        int
	hitOnce      bool

	interrupted bool
	// The traced data is written to this writer.
	outputWriter io.Writer
}

type goRoutineStatus struct {
	// This list include only the functions which hit the breakpoint before and so is not complete.
	callingFunctions []callingFunction
	panicking        bool
}

func (status goRoutineStatus) usedStackSize() uint64 {
	if len(status.callingFunctions) > 0 {
		return status.callingFunctions[len(status.callingFunctions)-1].usedStackSize
	}

	return 0
}

type callingFunction struct {
	*tracee.Function
	returnAddress uint64
	usedStackSize uint64
}

// NewController returns the new controller.
func NewController() *Controller {
	return &Controller{outputWriter: os.Stdout}
}

// LaunchTracee launches the new tracee process to be controlled.
func (c *Controller) LaunchTracee(name string, arg ...string) error {
	var err error
	c.statusStore = make(map[int]goRoutineStatus)
	c.process, err = tracee.LaunchProcess(name, arg...)
	return err
}

// AttachTracee attaches to the existing process.
func (c *Controller) AttachTracee(pid int) error {
	var err error
	c.statusStore = make(map[int]goRoutineStatus)
	c.process, err = tracee.AttachProcess(pid)
	return err
}

// SetTracingPoint sets the starting point of the tracing. The tracing is enabled when this function is called and disabled when it is returned.
// The tracing point can be set only once.
func (c *Controller) SetTracingPoint(functionName string) error {
	if c.tracingPoint != nil {
		return errors.New("tracing point is set already")
	}

	function, err := c.findFunction(functionName)
	if err != nil {
		return err
	}

	if !c.canSetBreakpoint(function) {
		return fmt.Errorf("can't set the tracing point for %s", functionName)
	}

	if err := c.process.SetBreakpoint(function.Value); err != nil {
		return err
	}

	c.tracingPoint = &tracingPoint{function: function}
	return nil
}

func (c *Controller) findFunction(functionName string) (*tracee.Function, error) {
	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return nil, err
	}

	for _, function := range functions {
		if function.Name == functionName {
			return function, nil
		}
	}
	return nil, errors.New("failed to find function")
}

func (c *Controller) canSetBreakpoint(function *tracee.Function) bool {
	allowedFuncs := []string{"runtime.deferproc", "runtime.gopanic", "runtime.gorecover"}
	for _, f := range allowedFuncs {
		if function.Name == f {
			return true
		}
	}

	// TODO: too conservative. At least funcs to operate map, chan, slice should be allowed.
	if strings.HasPrefix(function.Name, "runtime") && !function.IsExported() {
		return false
	}

	prefixesToAvoid := []string{"_rt0", "type."}
	for _, prefix := range prefixesToAvoid {
		if strings.HasPrefix(function.Name, prefix) {
			return false
		}
	}
	return true
}

// SetDepth sets the depth, which decides whether to print the traced info.
// It is the stack's relative depth from the point the tracing starts.
// For example, when the stack depth is 'x' at the start point, the called function info is printed
// if the stack depth is within 'x+depth'.
func (c *Controller) SetDepth(depth int) {
	c.depth = depth
}

// MainLoop repeatedly lets the tracee continue and then wait an event.
func (c *Controller) MainLoop() error {
	trappedThreadIDs, event, err := c.process.ContinueAndWait()
	if err != nil {
		return err
	}

	for {
		switch event.Type {
		case debugapi.EventTypeExited:
			return nil
		case debugapi.EventTypeCoreDump:
			return errors.New("the process exited due to core dump")
		case debugapi.EventTypeTerminated:
			return fmt.Errorf("the process exited due to signal %d", event.Data)
		case debugapi.EventTypeTrapped:
			trappedThreadIDs, event, err = c.handleTrapEvent(trappedThreadIDs)
			if err == ErrInterrupted {
				return err
			} else if err != nil {
				return fmt.Errorf("failed to handle trap event: %v", err)
			}
		default:
			return fmt.Errorf("unknown event: %v", event.Type)
		}
	}
}

func (c *Controller) handleTrapEvent(trappedThreadIDs []int) ([]int, debugapi.Event, error) {
	for _, threadID := range trappedThreadIDs {
		if err := c.handleTrapEventOfThread(threadID); err != nil {
			return nil, debugapi.Event{}, err
		}
	}

	if c.interrupted {
		if err := c.process.Detach(); err != nil {
			return nil, debugapi.Event{}, err
		}
		return nil, debugapi.Event{}, ErrInterrupted
	}

	return c.process.ContinueAndWait()
}

func (c *Controller) handleTrapEventOfThread(threadID int) error {
	goRoutineInfo, err := c.process.CurrentGoRoutineInfo(threadID)
	if err != nil {
		return err
	}

	if !c.process.HitBreakpoint(goRoutineInfo.CurrentPC-1, goRoutineInfo.ID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, goRoutineInfo)
	}

	goRoutineID := int(goRoutineInfo.ID)
	status, _ := c.statusStore[goRoutineID]
	if goRoutineInfo.UsedStackSize < status.usedStackSize() {
		return c.handleTrapAtFunctionReturn(threadID, goRoutineInfo)
	} else if goRoutineInfo.UsedStackSize == status.usedStackSize() {
		// it's likely we are in the same stack frame as before (typical for the stack growth case).
		return c.handleTrapAtUnrelatedBreakpoint(threadID, goRoutineInfo)
	}
	return c.handleTrapAtFunctionCall(threadID, goRoutineInfo)
}

func (c *Controller) handleTrapAtUnrelatedBreakpoint(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	trappedAddr := goRoutineInfo.CurrentPC - 1
	return c.process.SingleStep(threadID, trappedAddr)
}

func (c *Controller) handleTrapAtFunctionCall(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	goRoutineID := int(goRoutineInfo.ID)
	status, _ := c.statusStore[goRoutineID]
	currStackDepth := len(status.callingFunctions) + 1
	panicking := c.isPanicking(status)
	if panicking && goRoutineInfo.DeferedBy != nil {
		currStackDepth -= c.findNumFramesToSkip(status.callingFunctions, goRoutineInfo.DeferedBy.UsedStackSize)
	}

	if c.tracingPoint.Hit(goRoutineInfo.CurrentPC - 1) {
		if !c.hitOnce {
			if err := c.setBreakpointsExceptTracingPoint(); err != nil {
				return err
			}
			c.hitOnce = true
		}

		c.tracingPoint.Enter(goRoutineInfo.ID, currStackDepth)
	}

	stackFrame, err := c.currentStackFrame(goRoutineInfo)
	if err != nil {
		return err
	}

	if c.canPrint(goRoutineInfo.ID, currStackDepth) {
		if err := c.printFunctionInput(goRoutineID, stackFrame, currStackDepth); err != nil {
			return err
		}
	}

	if err := c.process.SetConditionalBreakpoint(stackFrame.ReturnAddress, goRoutineInfo.ID); err != nil {
		return err
	}

	funcAddr := stackFrame.Function.Value
	if err := c.process.SingleStep(threadID, funcAddr); err != nil {
		return err
	}

	callingFunc := callingFunction{
		Function:      stackFrame.Function,
		returnAddress: stackFrame.ReturnAddress,
		usedStackSize: goRoutineInfo.UsedStackSize,
	}
	c.statusStore[goRoutineID] = goRoutineStatus{
		callingFunctions: append(status.callingFunctions, callingFunc),
		panicking:        panicking,
	}
	return nil
}

func (c *Controller) isPanicking(status goRoutineStatus) bool {
	for _, function := range status.callingFunctions {
		if function.Name == "runtime.gopanic" {
			return true
		}
	}
	return false
}

func (c *Controller) findNumFramesToSkip(callingFuncs []callingFunction, usedStackSize uint64) int {
	for i := len(callingFuncs) - 1; i >= 0; i-- {
		callingFunc := callingFuncs[i]
		if callingFunc.usedStackSize < usedStackSize {
			return len(callingFuncs) - 1 - i
		}
	}
	return len(callingFuncs) - 1
}

func (c *Controller) setBreakpointsExceptTracingPoint() error {
	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return err
	}
	for _, function := range functions {
		if !c.canSetBreakpoint(function) || function.Name == c.tracingPoint.function.Name {
			continue
		}
		if err := c.process.SetBreakpoint(function.Value); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) canPrint(goRoutineID int64, currStackDepth int) bool {
	currRelativeDepth := c.tracingPoint.Depth(goRoutineID, currStackDepth)
	return c.tracingPoint.Inside(goRoutineID) && currRelativeDepth <= c.depth
}

func (c *Controller) handleTrapAtFunctionReturn(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	goRoutineID := int(goRoutineInfo.ID)
	status, _ := c.statusStore[goRoutineID]
	currStackDepth := len(status.callingFunctions)
	panicking := c.isPanicking(status)
	if panicking && goRoutineInfo.DeferedBy != nil {
		currStackDepth -= c.findNumFramesToSkip(status.callingFunctions, goRoutineInfo.DeferedBy.UsedStackSize)
	}

	if c.canPrint(goRoutineInfo.ID, currStackDepth) {
		function := status.callingFunctions[len(status.callingFunctions)-1].Function
		prevStackFrame, err := c.prevStackFrame(goRoutineInfo, function.Value)
		if err != nil {
			return err
		}
		if err := c.printFunctionOutput(goRoutineID, prevStackFrame, currStackDepth); err != nil {
			return err
		}

		if c.tracingPoint.Hit(function.Value) {
			c.tracingPoint.Exit(goRoutineInfo.ID, currStackDepth)
		}
	}

	breakpointAddr := goRoutineInfo.CurrentPC - 1

	if err := c.process.SingleStep(threadID, breakpointAddr); err != nil {
		return err
	}

	if err := c.process.ClearConditionalBreakpoint(breakpointAddr, goRoutineInfo.ID); err != nil {
		return err
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		callingFunctions: status.callingFunctions[0 : len(status.callingFunctions)-1],
		panicking:        panicking,
	}
	return nil
}

// It must be called at the beginning of the function, because it assumes rbp = rsp-8
func (c *Controller) currentStackFrame(goRoutineInfo tracee.GoRoutineInfo) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-8, goRoutineInfo.CurrentPC)
}

// It must be called at return address, because it assumes rbp = rsp-16
func (c *Controller) prevStackFrame(goRoutineInfo tracee.GoRoutineInfo, rip uint64) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-16, rip)
}

func (c *Controller) printFunctionInput(goRoutineID int, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.InputArguments {
		var value string
		switch arg.Typ.String() {
		case "int", "int64":
			value = strconv.Itoa(int(binary.LittleEndian.Uint64(arg.Value)))
		default:
			value = fmt.Sprintf("%v", arg.Value)
		}
		args = append(args, fmt.Sprintf("%s = %s", arg.Name, value))
	}

	fmt.Fprintf(c.outputWriter, "%s=> (#%02d) %s(%s)\n", strings.Repeat(" ", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

func (c *Controller) printFunctionOutput(goRoutineID int, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.OutputArguments {
		var value string
		switch arg.Typ.String() {
		case "int", "int64":
			value = strconv.Itoa(int(binary.LittleEndian.Uint64(arg.Value)))
		default:
			value = fmt.Sprintf("%v", arg.Value)
		}
		args = append(args, fmt.Sprintf("%s = %s", arg.Name, value))
	}
	fmt.Fprintf(c.outputWriter, "%s<= (#%02d) %s(...) (%s)\n", strings.Repeat(" ", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

// Interrupt interrupts the main loop.
func (c *Controller) Interrupt() {
	c.interrupted = true
}

type goRoutineInside struct {
	id int64
	// stackDepth is the depth of the stack when the tracing starts.
	stackDepth int
}

type tracingPoint struct {
	function         *tracee.Function
	goRoutinesInside []goRoutineInside
}

// Hit returns true if pc is same as tracing point.
func (p *tracingPoint) Hit(pc uint64) bool {
	return pc == p.function.Value
}

// Enter updates the list of the go routines which are inside the tracing point.
// It does nothing if the go routine has already entered.
func (p *tracingPoint) Enter(goRoutineID int64, stackDepth int) {
	for _, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID {
			return
		}
	}

	p.goRoutinesInside = append(p.goRoutinesInside, goRoutineInside{id: goRoutineID, stackDepth: stackDepth})
	return
}

// Exit removes the go routine from the inside-go routines list.
// Note that the go routine is not removed if the depth is different (to support recursive call's case).
func (p *tracingPoint) Exit(goRoutineID int64, stackDepth int) bool {
	for i, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID && goRoutine.stackDepth == stackDepth {
			p.goRoutinesInside = append(p.goRoutinesInside[0:i], p.goRoutinesInside[i+1:len(p.goRoutinesInside)]...)
			return true
		}
	}

	return false
}

// Inside returns true if the go routine is inside the tracing point.
func (p *tracingPoint) Inside(goRoutineID int64) bool {
	for _, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID {
			return true
		}
	}
	return false
}

// Depth returns the diff between the current stack depth and the depth when the tracing starts.
// It returns -1 if the go routine is not traced.
func (p *tracingPoint) Depth(goRoutineID int64, currDepth int) int {
	for _, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID {
			return currDepth - goRoutine.stackDepth
		}
	}

	return -1
}
