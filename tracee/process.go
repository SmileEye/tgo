package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/debugapi/lldb"
	"github.com/ks888/tgo/log"
)

var breakpointInsts = []byte{0xcc}

// Process represents the tracee process launched by or attached to this tracer.
type Process struct {
	debugapiClient *lldb.Client
	breakpoints    map[uint64]*breakpoint
	Binary         Binary
	valueParser    valueParser
}

type moduleData struct {
	types, etypes uint64
}

const countDisabled = -1

// StackFrame describes the data in the stack frame and its associated function.
type StackFrame struct {
	Function        *Function
	InputArguments  []Argument
	OutputArguments []Argument
	ReturnAddress   uint64
}

// LaunchProcess launches new tracee process.
func LaunchProcess(name string, arg ...string) (*Process, error) {
	debugapiClient := lldb.NewClient()
	if err := debugapiClient.LaunchProcess(name, arg...); err != nil {
		return nil, err
	}

	return newProcess(debugapiClient, name)
}

// AttachProcess attaches to the existing tracee process.
func AttachProcess(pid int) (*Process, error) {
	debugapiClient := lldb.NewClient()
	err := debugapiClient.AttachProcess(pid)
	if err != nil {
		return nil, err
	}

	programPath, err := findProgramPath(pid)
	if err != nil {
		return nil, err
	}

	return newProcess(debugapiClient, programPath)
}

func newProcess(debugapiClient *lldb.Client, programPath string) (*Process, error) {
	binary, err := NewBinary(programPath)
	if err != nil {
		return nil, err
	}

	mapRuntimeType, err := buildMapRuntimeTypeFunc(binary, debugapiClient)
	if err != nil {
		return nil, err
	}

	return &Process{
		debugapiClient: debugapiClient,
		breakpoints:    make(map[uint64]*breakpoint),
		Binary:         binary,
		valueParser:    valueParser{reader: debugapiClient, mapRuntimeType: mapRuntimeType},
	}, nil
}

func buildMapRuntimeTypeFunc(binary Binary, debugapiClient *lldb.Client) (func(runtimeTypeAddr uint64) (dwarf.Type, error), error) {
	firstModuleDataAddr, err := binary.findFirstModuleDataAddress()
	if err != nil {
		return nil, err
	}

	moduleDataList, err := parseModuleDataList(firstModuleDataAddr, debugapiClient)
	if err != nil {
		return nil, err
	}

	return func(runtimeTypeAddr uint64) (dwarf.Type, error) {
		var md moduleData
		for _, candidate := range moduleDataList {
			if candidate.types <= runtimeTypeAddr && runtimeTypeAddr < candidate.etypes {
				md = candidate
				break
			}
		}

		implTypOffset := binary.types[runtimeTypeAddr-md.types]
		return binary.dwarf.Type(implTypOffset)
	}, nil
}

func parseModuleDataList(firstModuleDataAddr uint64, debugapiClient *lldb.Client) ([]moduleData, error) {
	var moduleDataList []moduleData
	buff := make([]byte, 8)
	moduleDataAddr := firstModuleDataAddr
	for {
		// TODO: use the DIE of the moduleData type
		var offsetToTypes uint64 = 24*3 + 8*16
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToTypes, buff); err != nil {
			return nil, err
		}
		types := binary.LittleEndian.Uint64(buff)

		var offsetToEtypes uint64 = 24*3 + 8*17
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToEtypes, buff); err != nil {
			return nil, err
		}
		etypes := binary.LittleEndian.Uint64(buff)

		moduleDataList = append(moduleDataList, moduleData{types: types, etypes: etypes})

		var offsetToNext uint64 = 24*3 + 8*18 + 24*4 + 16 + 24 + 16 + 24 + 8 + (4+8)*2 + 8 + 1
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToNext, buff); err != nil {
			return nil, err
		}
		next := binary.LittleEndian.Uint64(buff)
		if next == 0 {
			return moduleDataList, nil
		}

		moduleDataAddr = next
	}
}

// Detach detaches from the tracee process. All breakpoints are cleared.
func (p *Process) Detach() error {
	for breakpointAddr := range p.breakpoints {
		if err := p.ClearBreakpoint(breakpointAddr); err != nil {
			return err
		}
	}

	if err := p.debugapiClient.DetachProcess(); err != nil {
		return err
	}

	return p.close()
}

func (p *Process) close() error {
	return p.Binary.Close()
}

// ContinueAndWait continues the execution and waits until an event happens.
// Note that the id of the stopped thread may be different from the id of the continued thread.
func (p *Process) ContinueAndWait() (debugapi.Event, error) {
	event, err := p.debugapiClient.ContinueAndWait()
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
}

// SingleStep executes one instruction while clearing and setting breakpoints.
func (p *Process) SingleStep(threadID int, trappedAddr uint64) error {
	if err := p.setPC(threadID, trappedAddr); err != nil {
		return err
	}

	bp, bpSet := p.breakpoints[trappedAddr]
	if bpSet {
		if err := p.debugapiClient.WriteMemory(trappedAddr, bp.orgInsts); err != nil {
			return err
		}
	}

	if _, err := p.stepAndWait(threadID); err != nil {
		unspecifiedError, ok := err.(debugapi.UnspecifiedThreadError)
		if !ok {
			return err
		}

		if err := p.singleStepUnspecifiedThreads(threadID, unspecifiedError); err != nil {
			return err
		}
		return p.SingleStep(threadID, trappedAddr)
	}

	if bpSet {
		return p.debugapiClient.WriteMemory(trappedAddr, breakpointInsts)
	}
	return nil
}

func (p *Process) setPC(threadID int, addr uint64) error {
	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return err
	}

	regs.Rip = addr
	return p.debugapiClient.WriteRegisters(threadID, regs)
}

func (p *Process) stepAndWait(threadID int) (event debugapi.Event, err error) {
	event, err = p.debugapiClient.StepAndWait(threadID)
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
}

// SetBreakpoint sets the breakpoint at the specified address.
func (p *Process) SetBreakpoint(addr uint64) error {
	return p.SetConditionalBreakpoint(addr, 0)
}

// ClearBreakpoint clears the breakpoint at the specified address.
func (p *Process) ClearBreakpoint(addr uint64) error {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return nil
	}

	if err := p.debugapiClient.WriteMemory(addr, bp.orgInsts); err != nil {
		return err
	}

	delete(p.breakpoints, addr)
	return nil
}

// SetConditionalBreakpoint sets the breakpoint which only the specified go routine hits.
func (p *Process) SetConditionalBreakpoint(addr uint64, goRoutineID int64) error {
	bp, ok := p.breakpoints[addr]
	if ok {
		if goRoutineID != 0 {
			bp.AddTarget(goRoutineID)
		}
		return nil
	}

	originalInsts := make([]byte, len(breakpointInsts))
	if err := p.debugapiClient.ReadMemory(addr, originalInsts); err != nil {
		return err
	}
	if err := p.debugapiClient.WriteMemory(addr, breakpointInsts); err != nil {
		return err
	}

	bp = newBreakpoint(addr, originalInsts)
	if goRoutineID != 0 {
		bp.AddTarget(goRoutineID)
	}
	p.breakpoints[addr] = bp
	return nil
}

// ClearConditionalBreakpoint clears the conditional breakpoint at the specified address and go routine.
// The breakpoint may still exist on memory if there are other go routines not cleared.
// Use SingleStep() to temporary disable the breakpoint.
func (p *Process) ClearConditionalBreakpoint(addr uint64, goRoutineID int64) error {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return nil
	}
	bp.RemoveTarget(goRoutineID)

	if !bp.NoTarget() {
		return nil
	}

	return p.ClearBreakpoint(addr)
}

// HitBreakpoint checks the current go routine meets the condition of the breakpoint.
func (p *Process) HitBreakpoint(addr uint64, goRoutineID int64) bool {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return false
	}

	return bp.Hit(goRoutineID)
}

// HasBreakpoint returns true if the the breakpoint is already set at the specified address.
func (p *Process) HasBreakpoint(addr uint64) bool {
	_, ok := p.breakpoints[addr]
	return ok
}

// StackFrameAt returns the stack frame to which the given rbp specified.
// To get the correct stack frame, it assumes:
// * rsp points to the return address.
// * rsp+8 points to the beginning of the args list.
//
// To be accurate, we need to check the .debug_frame section to find the CFA and return address.
// But we omit the check here because this function is called at only the beginning or end of the tracee's function call.
func (p *Process) StackFrameAt(rsp, rip uint64) (*StackFrame, error) {
	function, err := p.Binary.FindFunction(rip)
	if err != nil {
		return nil, err
	}

	buff := make([]byte, 8)
	if err := p.debugapiClient.ReadMemory(rsp, buff); err != nil {
		return nil, err
	}
	retAddr := binary.LittleEndian.Uint64(buff)

	inputArgs, outputArgs, err := p.currentArgs(function.Parameters, rsp+8)
	if err != nil {
		return nil, err
	}

	return &StackFrame{
		Function:        function,
		ReturnAddress:   retAddr,
		InputArguments:  inputArgs,
		OutputArguments: outputArgs,
	}, nil
}

func (p *Process) currentArgs(params []Parameter, addrBeginningOfArgs uint64) (inputArgs []Argument, outputArgs []Argument, err error) {
	for _, param := range params {
		param := param // without this, all the closures point to the last param.
		parseValue := func(depth int) value {
			if !param.Exist {
				return nil
			}

			size := param.Typ.Size()
			buff := make([]byte, size)
			if err = p.debugapiClient.ReadMemory(addrBeginningOfArgs+uint64(param.Offset), buff); err != nil {
				log.Debugf("failed to read the '%s' value: %v", param.Name, err)
				return nil
			}
			return p.valueParser.parseValue(param.Typ, buff, depth)
		}

		arg := Argument{Name: param.Name, Typ: param.Typ, parseValue: parseValue}
		if param.IsOutput {
			outputArgs = append(outputArgs, arg)
		} else {
			inputArgs = append(inputArgs, arg)
		}
	}
	return
}

// GoRoutineInfo describes the various info of the go routine like pc.
type GoRoutineInfo struct {
	ID               int64
	UsedStackSize    uint64
	CurrentPC        uint64
	CurrentStackAddr uint64
	Panicking        bool
	PanicHandler     *PanicHandler
	Ancestors        []int64
}

// PanicHandler holds the function info which (will) handles panic.
type PanicHandler struct {
	// UsedStackSizeAtDefer and PCAtDefer are the function info which register this handler by 'defer'.
	UsedStackSizeAtDefer uint64
	PCAtDefer            uint64
}

// CurrentGoRoutineInfo returns the go routine info associated with the go routine which hits the breakpoint.
func (p *Process) CurrentGoRoutineInfo(threadID int) (GoRoutineInfo, error) {
	gAddr, err := p.debugapiClient.ReadTLS(threadID, p.offsetToG())
	if err != nil {
		unspecifiedError, ok := err.(debugapi.UnspecifiedThreadError)
		if !ok {
			return GoRoutineInfo{}, err
		}

		if err := p.singleStepUnspecifiedThreads(threadID, unspecifiedError); err != nil {
			return GoRoutineInfo{}, err
		}
		return p.CurrentGoRoutineInfo(threadID)
	}

	_, idRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType, "goid")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	id := int64(binary.LittleEndian.Uint64(idRawVal))

	stackType, stackRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType, "stack")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	stackVal := p.valueParser.parseValue(stackType, stackRawVal, 1)
	stackHi := stackVal.(structValue).fields["hi"].(uint64Value).val

	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return GoRoutineInfo{}, err
	}
	usedStackSize := stackHi - regs.Rsp

	_, panicRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType, "_panic")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	panicAddr := binary.LittleEndian.Uint64(panicRawVal)
	panicking := panicAddr != 0

	panicHandler, err := p.findPanicHandler(gAddr, panicAddr, stackHi)
	if err != nil {
		return GoRoutineInfo{}, err
	}

	return GoRoutineInfo{ID: id, UsedStackSize: usedStackSize, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp, Panicking: panicking, PanicHandler: panicHandler /*, Ancestors: ancestors */}, nil

	// offsetToAncestors := uint64(p.Binary.runtimeGFields["ancestors"])
	// if err = p.debugapiClient.ReadMemory(gAddr+offsetToAncestors, buff); err != nil {
	// 	return GoRoutineInfo{}, err
	// }
	// ancestorsAddr := binary.LittleEndian.Uint64(buff)
	// ancestors, err := p.findAncestors(ancestorsAddr)
	// if err != nil {
	// 	return GoRoutineInfo{}, err
	// }

	// size := p.Binary.runtimeGType.Size()
	// buff := make([]byte, size)
	// if err = p.debugapiClient.ReadMemory(gAddr, buff); err != nil {
	// 	return fmt.Errorf("failed to read runtime.g value: %v", err)
	// }
	// return p.valueParser.parseValue(param.Typ, buff, 1)
}

// TODO: depend on os
func (p *Process) offsetToG() uint32 {
	if p.Binary.goVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11}) {
		return 0x30
	}
	return 0x8a0
}

func (p *Process) singleStepUnspecifiedThreads(threadID int, err debugapi.UnspecifiedThreadError) error {
	for _, unspecifiedThread := range err.ThreadIDs {
		if unspecifiedThread == threadID {
			continue
		}

		regs, err := p.debugapiClient.ReadRegisters(unspecifiedThread)
		if err != nil {
			return err
		}
		if err := p.SingleStep(unspecifiedThread, regs.Rip-1); err != nil {
			return err
		}
	}
	return nil
}

func (p *Process) findFieldInStruct(structAddr uint64, structType dwarf.Type, fieldName string) (dwarf.Type, []byte, error) {
	for {
		typedefType, ok := structType.(*dwarf.TypedefType)
		if !ok {
			break
		}
		structType = typedefType.Type
	}

	for _, field := range structType.(*dwarf.StructType).Field {
		if field.Name != fieldName {
			continue
		}

		buff := make([]byte, field.Type.Size())
		if err := p.debugapiClient.ReadMemory(structAddr+uint64(field.ByteOffset), buff); err != nil {
			return nil, nil, fmt.Errorf("failed to read memory: %v", err)
		}
		return field.Type, buff, nil
	}
	return nil, nil, fmt.Errorf("field %s not found", fieldName)
}

func (p *Process) findPanicHandler(gAddr, panicAddr, stackHi uint64) (*PanicHandler, error) {
	ptrToDeferType, rawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType, "_defer")
	if err != nil {
		return nil, err
	}
	deferAddr := binary.LittleEndian.Uint64(rawVal)
	deferType := ptrToDeferType.(*dwarf.PtrType).Type

	for deferAddr != 0 {
		_, rawVal, err := p.findFieldInStruct(deferAddr, deferType, "_panic")
		if err != nil {
			return nil, err
		}
		panicInDefer := binary.LittleEndian.Uint64(rawVal)
		if panicInDefer == panicAddr {
			break
		}

		_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "link")
		if err != nil {
			return nil, err
		}
		deferAddr = binary.LittleEndian.Uint64(rawVal)
	}

	if deferAddr == 0 {
		return nil, nil
	}

	_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "sp")
	if err != nil {
		return nil, err
	}
	stackAddress := binary.LittleEndian.Uint64(rawVal)
	usedStackSizeAtDefer := stackHi - stackAddress

	_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "pc")
	if err != nil {
		return nil, err
	}
	pc := binary.LittleEndian.Uint64(rawVal)

	return &PanicHandler{UsedStackSizeAtDefer: usedStackSizeAtDefer, PCAtDefer: pc}, nil
}

func (p *Process) findAncestors(ancestorsAddr uint64) ([]int64, error) {
	return nil, nil
}

// ThreadInfo describes the various info of thread.
type ThreadInfo struct {
	ID               int
	CurrentPC        uint64
	CurrentStackAddr uint64
}

// CurrentThreadInfo returns the thread info of the specified thread ID.
func (p *Process) CurrentThreadInfo(threadID int) (ThreadInfo, error) {
	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return ThreadInfo{}, err
	}
	return ThreadInfo{ID: threadID, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp}, nil
}

// Argument represents the value passed to the function.
type Argument struct {
	Name string
	Typ  dwarf.Type
	// parseValue lazily parses the value. The parsing every time is not only wasting resource, but the value may not be initialized yet.
	parseValue func(int) value
}

// ParseValue parses the arg value and returns string representation.
// The `depth` option specifies to the depth of the parsing.
func (arg Argument) ParseValue(depth int) string {
	val := arg.parseValue(depth)
	if val == nil {
		return fmt.Sprintf("%s = -", arg.Name)
	}
	return fmt.Sprintf("%s = %s", arg.Name, val)
}

type breakpoint struct {
	addr     uint64
	orgInsts []byte
	// targetGoRoutineIDs are go routine ids interested in this breakpoint.
	// Empty list implies all the go routines are target.
	targetGoRoutineIDs []int64
}

func newBreakpoint(addr uint64, orgInsts []byte) *breakpoint {
	return &breakpoint{addr: addr, orgInsts: orgInsts}
}

func (bp *breakpoint) AddTarget(goRoutineID int64) {
	bp.targetGoRoutineIDs = append(bp.targetGoRoutineIDs, goRoutineID)
	return
}

func (bp *breakpoint) RemoveTarget(goRoutineID int64) {
	for i, candidate := range bp.targetGoRoutineIDs {
		if candidate == goRoutineID {
			bp.targetGoRoutineIDs = append(bp.targetGoRoutineIDs[0:i], bp.targetGoRoutineIDs[i+1:len(bp.targetGoRoutineIDs)]...)
			return
		}
	}
	return
}

func (bp *breakpoint) NoTarget() bool {
	return len(bp.targetGoRoutineIDs) == 0
}

func (bp *breakpoint) Hit(goRoutineID int64) bool {
	for _, existingID := range bp.targetGoRoutineIDs {
		if existingID == goRoutineID {
			return true
		}
	}

	return len(bp.targetGoRoutineIDs) == 0
}
