package lldb

import (
	"net"
	"reflect"
	"testing"

	"github.com/ks888/tgo/debugapi"
)

var (
	infloopProgram           = "testdata/infloop"
	beginTextSection uintptr = 0x1000000
	mainAddr         uintptr = 0x1051430
)

// TODO: terminate process

func TestLaunchProcess(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if tid == 0 {
		t.Errorf("invalid tid: %d", tid)
	}
}

func TestLaunchProcess_ProgramNotExist(t *testing.T) {
	client := NewClient()
	_, err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatalf("error not returned")
	}
}

func TestReadRegisters(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	regs, err := client.ReadRegisters(tid)
	if err != nil {
		t.Fatalf("failed to read registers: %v", err)
	}
	if regs.Rip == 0 {
		t.Fatalf("empty rip: %x", regs.Rip)
	}
	if regs.Rsp == 0 {
		t.Fatalf("empty rsp: %x", regs.Rsp)
	}
}

func TestWriteRegisters(t *testing.T) {
	client := NewClient()
	tid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	regs := debugapi.Registers{Rip: 0x1, Rsp: 0x2}
	if err := client.WriteRegisters(tid, regs); err != nil {
		t.Fatalf("failed to write registers: %v", err)
	}

	actualRegs, _ := client.ReadRegisters(tid)
	if actualRegs.Rip != 0x1 {
		t.Errorf("wrong rip: %x", actualRegs.Rip)
	}
	if actualRegs.Rsp != 0x2 {
		t.Errorf("wrong rsp: %x", actualRegs.Rsp)
	}
}

func TestSetNoAckMode(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "QStartNoAckMode" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("OK"); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)

	if err := client.setNoAckMode(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !client.noAckMode {
		t.Errorf("ack mode is not set")
	}

	<-sendDone
}

func TestSetNoAckMode_ErrorReturned(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		_, _ = client.receive()
		_ = client.send("E00")
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)

	if err := client.setNoAckMode(); err == nil {
		t.Errorf("error is not returned")
	}

	<-sendDone
}

func TestQSupported(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qSupported:swbreak+;hwbreak+;no-resumed+" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("qXfer:features:read+;PacketSize=20000;qEcho+"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	if err := client.qSupported(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestCollectRegisterMetadata(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		_, _ = client.receive()
		_ = client.send("name:rax;bitsize:64;offset:0;")
		_, _ = client.receive()
		_ = client.send("name:rbx;bitsize:64;offset:8;")
		_, _ = client.receive()
		_ = client.send("E45")

	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	meatadata, err := client.collectRegisterMetadata()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meatadata) != 2 {
		t.Errorf("wrong length of register metadata: %d", len(meatadata))
	}

	<-sendDone
}

func TestQRegisterInfo(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qRegisterInfo0" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("name:rax;bitsize:64;offset:0;encoding:uint;format:hex;set:General Purpose Registers;ehframe:0;dwarf:0;invalidate-regs:0,15,25,35,39;"); err != nil {
			t.Fatalf("failed to send response: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	reg, err := client.qRegisterInfo(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.name != "rax" {
		t.Errorf("wrong name: %s", reg.name)
	}
	if reg.offset != 0 {
		t.Errorf("wrong offset: %d", reg.offset)
	}
	if reg.size != 8 {
		t.Errorf("wrong size: %d", reg.size)
	}

	<-sendDone
}

func TestQRegisterInfo_EndOfRegisterList(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		_, _ = client.receive()
		_ = client.send("E45")
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	_, err := client.qRegisterInfo(0)
	if err != errEndOfList {
		t.Fatalf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestQListThreadsInStopReply(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "QListThreadsInStopReply" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("OK"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	if err := client.qListThreadsInStopReply(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	<-sendDone
}

func TestQfThreadInfo(t *testing.T) {
	connForReceive, connForSend := net.Pipe()

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if data, err := client.receive(); err != nil {
			t.Fatalf("failed to receive command: %v", err)
		} else if data != "qfThreadInfo" {
			t.Errorf("unexpected data: %s", data)
		}

		if err := client.send("m15296fb"); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)

	tid, err := client.qfThreadInfo()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tid != "15296fb" {
		t.Errorf("unexpected tid: %v", tid)
	}

	<-sendDone
}

func TestSendAndReceive(t *testing.T) {
	connForReceive, connForSend := net.Pipe()
	cmd := "command"

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, false)
		if err := client.send(cmd); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, false)
	buff, err := client.receive()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != buff {
		t.Errorf("receieved unexpected data: %v", buff)
	}

	<-sendDone
}

func TestSendAndReceive_NoAckMode(t *testing.T) {
	connForReceive, connForSend := net.Pipe()
	cmd := "command"

	sendDone := make(chan bool)
	go func(conn net.Conn, ch chan bool) {
		defer close(ch)

		client := newTestClient(conn, true)
		if err := client.send(cmd); err != nil {
			t.Fatalf("failed to send command: %v", err)
		}
	}(connForSend, sendDone)

	client := newTestClient(connForReceive, true)
	buff, err := client.receive()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != buff {
		t.Errorf("receieved unexpected data: %v", buff)
	}

	<-sendDone
}

func TestVerifyPacket(t *testing.T) {
	for i, test := range []struct {
		packet      string
		expectError bool
	}{
		{packet: "$command#df", expectError: false},
		{packet: "#command#df", expectError: true},
		{packet: "$command$df", expectError: true},
		{packet: "$command#00", expectError: true},
	} {
		actual := verifyPacket(test.packet)
		if test.expectError && actual == nil {
			t.Errorf("[%d] error not returned", i)
		} else if !test.expectError && actual != nil {
			t.Errorf("[%d] error returned: %v", i, actual)
		}
	}
}

func TestHexToBytes(t *testing.T) {
	for i, test := range []struct {
		data     string
		expected []byte
	}{
		{data: "aa", expected: []byte{0xaa}},
		{data: "aabb", expected: []byte{0xaa, 0xbb}},
		{data: "", expected: nil},
	} {
		actual, err := hexToBytes(test.data)
		if err != nil {
			t.Fatalf("error returned: %v", err)
		}
		if !reflect.DeepEqual(actual, test.expected) {
			t.Errorf("[%d] failed to decode hex array: %v, %v", i, actual, test.expected)
		}
	}
}

func TestHexToBytes_NotHexArray(t *testing.T) {
	_, err := hexToBytes("zz")
	if err == nil {
		t.Fatalf("error not returned: %v", err)
	}
}

func TestHexToBytes_TooShortArray(t *testing.T) {
	_, err := hexToBytes("z")
	if err == nil {
		t.Fatalf("error not returned: %v", err)
	}
}

func TestChecksum(t *testing.T) {
	for i, data := range []struct {
		input    []byte
		expected uint8
	}{
		{input: []byte{0x1, 0x2}, expected: 3},
		{input: []byte{0x7f, 0x80}, expected: 255},
		{input: []byte{0x80, 0x80}, expected: 0},
		{input: []byte{0x63, 0x6f, 0x6d, 0x6d, 0x61, 0x6e, 0x64}, expected: 0xdf},
	} {
		sum := calcChecksum(data.input)
		if sum != data.expected {
			t.Errorf("[%d] wrong checksum: %x", i, sum)
		}
	}
}

func newTestClient(conn net.Conn, noAckMode bool) *Client {
	return &Client{conn: conn, noAckMode: noAckMode, buffer: make([]byte, maxPacketSize)}
}
