package tracee

import (
	"encoding/binary"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestLaunchProcess(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if proc.debugapiClient == nil {
		t.Errorf("debugapiClient is nil")
	}
	if proc.currentThreadID == 0 {
		t.Errorf("currentThreadID is 0")
	}
}

func TestDetach(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := proc.Detach(); err != nil {
		t.Fatalf("failed to detach process: %v", err)
	}

	if proc.HasBreakpoint(testutils.HelloworldAddrNoParameter) {
		t.Errorf("breakpoint still exists")
	}
}

func TestContinueAndWait(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	// 1. stop at NoParameter func
	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	if _, err := proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if err := proc.ClearBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	if err := proc.SetPC(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	// 2. stop at OneParameter func
	if err := proc.SetBreakpoint(testutils.HelloworldAddrOneParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	if _, err := proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestSetBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestSetBreakpoint_AlreadySet(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, expected)

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestSetConditionalBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestClearBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, expected)

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestClearBreakpoint_BPNotSet(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Errorf("failed to clear breakpoint: %v", err)
	}
}

func TestHitBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(testutils.HelloworldAddrMain, 1) {
		t.Errorf("invalid condition check")
	}

	if proc.HitBreakpoint(testutils.HelloworldAddrMain, 2) {
		t.Errorf("invalid condition check")
	}
}

func TestHitBreakpoint_NoCondition(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(testutils.HelloworldAddrMain, 1) {
		t.Errorf("invalid condition check")
	}
}

func TestSetPC(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := proc.SetBreakpoint(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if _, err = proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	if err := proc.SetPC(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set PC: %v", err)
	}

	if _, err = proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	regs, err := proc.debugapiClient.ReadRegisters(proc.currentThreadID)
	if err != nil {
		t.Fatalf("failed to read registers: %v", err)
	}
	if regs.Rip != testutils.HelloworldAddrMain+1 {
		t.Errorf("wrong rip: %x", regs.Rip)
	}
}

func TestStackFrameAt(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := proc.SetBreakpoint(testutils.HelloworldAddrOneParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if _, err = proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	regs, err := proc.debugapiClient.ReadRegisters(proc.currentThreadID)
	if err != nil {
		t.Fatalf("failed to read registers: %v", err)
	}

	stackFrame, err := proc.StackFrameAt(regs.Rsp-8, regs.Rip)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if stackFrame.Function.Name != "main.oneParameter" {
		t.Errorf("wrong function name: %s", stackFrame.Function.Name)
	}
	if stackFrame.ReturnAddress == 0x0 {
		t.Errorf("empty return address")
	}
	if len(stackFrame.InputArguments) != 1 {
		t.Errorf("wrong input args length: %d", len(stackFrame.InputArguments))
	}
	if stackFrame.InputArguments[0].Name != "a" {
		t.Errorf("wrong input args: %s", stackFrame.InputArguments[0].Name)
	}
	if binary.LittleEndian.Uint64(stackFrame.InputArguments[0].Value) != 1 {
		t.Errorf("wrong input args: %s", stackFrame.InputArguments[0].Value)
	}
	if len(stackFrame.OutputArguments) != 0 {
		t.Errorf("wrong output args length: %d", len(stackFrame.OutputArguments))
	}
}

func TestCurrentGoRoutineInfo(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := proc.SetBreakpoint(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if _, err = proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	goRoutineInfo, err := proc.CurrentGoRoutineInfo()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if goRoutineInfo.ID != 1 {
		t.Errorf("wrong id: %d", goRoutineInfo.ID)
	}
	if goRoutineInfo.UsedStackSize == 0 {
		t.Errorf("wrong stack size: %d", goRoutineInfo.UsedStackSize)
	}
	if goRoutineInfo.CurrentPC != testutils.HelloworldAddrMain+1 {
		t.Errorf("empty return address: %d", goRoutineInfo.CurrentPC)
	}
	if goRoutineInfo.CurrentStackAddr == 0 {
		t.Errorf("empty stack address: %d", goRoutineInfo.CurrentStackAddr)
	}
}
