/*
Copyright 2015 Google Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package broker

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"time"

	http2 "github.com/bradfitz/http2"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	emulators "google/emulators"
)

const (
	// The name of the environment variable with the broker's address.
	BrokerAddressEnv = "TESTENV_BROKER_ADDRESS"
)

var (
	// The HTTP/2 client preface
	http2ClientPreface = []byte(http2.ClientPreface)
)

func RunProcessTree(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Run()
}

// Use a session to group the child and its subprocesses, if any, for the
// purpose of terminating them as a group.
func StartProcessTree(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func KillProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	gid := -cmd.Process.Pid
	return syscall.Kill(gid, syscall.SIGINT)
}

type PortPicker interface {
	// Returns the next free port.
	Next() (int, error)
}

// Picks ports from a list of non-overlapping PortRange values.
type PortRangePicker struct {
	ranges []*emulators.PortRange
	rIndex int
	last   int
}

func (p *PortRangePicker) Next() (int, error) {
	if p.last == -1 || p.last+1 >= int(p.ranges[p.rIndex].End) {
		if p.rIndex >= len(p.ranges)-1 {
			return -1, fmt.Errorf("Exhausted all ranges")
		}
		p.rIndex++
		p.last = int(p.ranges[p.rIndex].Begin)
	} else {
		p.last++
	}
	return p.last, nil
}

// Implements sort.Interface for []emulators.PortRange based on Begin.
type byBegin []*emulators.PortRange

func (a byBegin) Len() int           { return len(a) }
func (a byBegin) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byBegin) Less(i, j int) bool { return a[i].Begin < a[j].Begin }

func NewPortRangePicker(ranges []*emulators.PortRange) (*PortRangePicker, error) {
	// Sort the ranges, so we can easily detect overlaps.
	sort.Sort(byBegin(ranges))
	for i, r := range ranges {
		if r.Begin <= 0 {
			return nil, fmt.Errorf("Invalid PortRange: %s", r)
		}
		if r.End <= r.Begin {
			return nil, fmt.Errorf("Invalid PortRange: %s", r)
		}
		if i > 0 {
			prev := ranges[i-1]
			if r.Begin < prev.End {
				return nil, fmt.Errorf("Overlapping PortRange: %s, %s", r, prev)
			}
		}
	}
	return &PortRangePicker{ranges: ranges, rIndex: -1, last: -1}, nil
}

type FreePortPicker struct{}

// Picks free ports.
func (p *FreePortPicker) Next() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	lis, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer lis.Close()
	return lis.Addr().(*net.TCPAddr).Port, nil
}

type BrokerClientConnection struct {
	emulators.BrokerClient
	conn *grpc.ClientConn
}

func NewBrokerClientConnection(timeout time.Duration) (*BrokerClientConnection, error) {
	brokerAddress := os.Getenv(BrokerAddressEnv)
	if brokerAddress == "" {
		return nil, fmt.Errorf("%s not specified", BrokerAddressEnv)
	}
	conn, err := grpc.Dial(brokerAddress, grpc.WithInsecure(), grpc.WithTimeout(timeout))
	if err != nil {
		log.Printf("failed to dial broker: %v", err)
		return nil, err
	}
	client := emulators.NewBrokerClient(conn)

	return &BrokerClientConnection{client, conn}, nil
}

func (bcc *BrokerClientConnection) RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	resp, err := bcc.BrokerClient.ListEmulators(ctx, EmptyPb)
	if err != nil {
		log.Printf("failed to list emulators: %v", err)
		return err
	}
	for _, emu := range resp.Emulators {
		if emu.Rule.RuleId != ruleId {
			continue
		}
		_, err = bcc.BrokerClient.ReportEmulatorOnline(ctx,
			&emulators.ReportEmulatorOnlineRequest{EmulatorId: emu.EmulatorId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address})
		if err != nil {
			log.Printf("failed to register emulator %q with broker: %v", emu.EmulatorId, err)
			return err
		}
		log.Printf("registered emulator %q with broker", emu.EmulatorId)
		return nil
	}
	_, err = bcc.BrokerClient.CreateResolveRule(ctx, &emulators.ResolveRule{RuleId: ruleId, TargetPatterns: additionalTargetPatterns, ResolvedTarget: address})
	if err != nil {
		log.Printf("failed to register rule %q with broker: %v", ruleId, err)
		return err
	}
	log.Printf("registered rule %q with broker", ruleId)
	return nil
}

func (bcc *BrokerClientConnection) CreateOrUpdateRegistrationRule(ruleId string, targetPatterns []string, address string, timeout time.Duration) error {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	_, err := bcc.BrokerClient.CreateResolveRule(ctx, &emulators.ResolveRule{RuleId: ruleId, TargetPatterns: targetPatterns, ResolvedTarget: address})
	if err != nil {
		log.Printf("failed to register rule %q with broker: %v", ruleId, err)
		return err
	}
	log.Printf("registered rule %q with broker", ruleId)
	return nil
}

func (bcc *BrokerClientConnection) Close() error {
	return bcc.conn.Close()
}

// Shortcut
func RegisterWithBroker(ruleId string, address string, additionalTargetPatterns []string, timeout time.Duration) error {
	bcc, err := NewBrokerClientConnection(timeout)
	if err != nil {
		return err
	}
	defer bcc.Close()
	return bcc.RegisterWithBroker(ruleId, address, additionalTargetPatterns, timeout)

}

// Returns the combined contents of a and b, with no duplicates.
func merge(a []string, b []string) []string {
	values := make(map[string]bool)
	for _, v := range a {
		values[v] = true
	}
	for _, v := range b {
		values[v] = true
	}
	var results []string
	for v, _ := range values {
		results = append(results, v)
	}
	return results
}

// Returns whether the values in a are the same as the values in b, when order
// is ignored.
func unorderedEqual(a []string, b []string) bool {
	aa := make(map[string]bool)
	bb := make(map[string]bool)
	for _, v := range a {
		aa[v] = true
	}
	for _, v := range b {
		bb[v] = true
	}
	return reflect.DeepEqual(aa, bb)
}

// Multiplexes between HTTP/1.x and HTTP/2 connections. Delegates to L.
type listenerMux struct {
	// Receives only HTTP/1.x connections.
	HTTPListener net.Listener

	// Receives only HTTP/2 connections.
	HTTP2Listener net.Listener

	// The underlying listener.
	delegate net.Listener
}

func newListenerMux(delegate net.Listener) *listenerMux {
	mux := listenerMux{
		HTTPListener:  newConnectionQueue(delegate.Addr()),
		HTTP2Listener: newConnectionQueue(delegate.Addr()),
		delegate:      delegate}
	go mux.run()
	return &mux
}

func (mux *listenerMux) run() error {
	// Code lifted from http module's Serve().
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		conn, e := mux.delegate.Accept()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				//srv.logf("http: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0

		// Our code.
		connWrapper := newConnWrapper(conn)
		has2, err := connWrapper.tryReadHTTP2Preface()
		if err != nil {
			connWrapper.Close()
			continue
		}
		if has2 {
			go mux.HTTP2Listener.(*connectionQueue).add(connWrapper)
		} else {
			go mux.HTTPListener.(*connectionQueue).add(connWrapper)
		}
	}
	return nil
}

func (mux *listenerMux) Close() error {
	err1 := mux.HTTPListener.Close()
	err2 := mux.HTTP2Listener.Close()
	err3 := mux.delegate.Close()
	if err3 != nil {
		return err3
	}
	if err2 != nil {
		return err2
	}
	return err1
}

// Decorates net.Conn. Supports detecting HTTP/2.
type connWrapper struct {
	preface     []byte
	readPreface bool
	pos         int
	net.Conn    // embedded (delegate)
}

func newConnWrapper(delegate net.Conn) *connWrapper {
	return &connWrapper{
		preface:     make([]byte, len(http2ClientPreface)),
		readPreface: false,
		pos:         0,
		Conn:        delegate}
}

// Read from the preface buffer if it has not yet been read. Otherwise, read
// normally.
func (c *connWrapper) Read(b []byte) (int, error) {
	if !c.readPreface {
		_, err := c.tryReadHTTP2Preface()
		if err != nil {
			return 0, err
		}
	}
	i := 0
	for i < len(b) && c.pos < len(c.preface) {
		b[i] = c.preface[c.pos]
		i++
		c.pos++
	}
	n, err := c.Conn.Read(b[i:])
	n += i
	//log.Printf("Read(): %d, %v, %v", n, err, b)
	return n, err
}

// Attempts to read an HTTP/2 prefix from the connection, returning true iff
// the preface is found.
// Subsequent calls to Read() will return results as if this method had not
// been called.
func (c *connWrapper) tryReadHTTP2Preface() (bool, error) {
	if !c.readPreface {
		// Code based on http2 module's readPreface().
		errc := make(chan error, 1)
		go func() {
			// Read the client preface
			_, err := io.ReadFull(c.Conn, c.preface)
			errc <- err
		}()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			return false, errors.New("timeout waiting for client preface")
		case err := <-errc:
			if err != nil {
				//log.Printf("Error reading preface: %v", err)
				return false, err
			}
			c.readPreface = true
			break
		}
	}
	if bytes.Equal(c.preface, http2ClientPreface) {
		//log.Printf("HTTP2 preface: %v", c.preface)
		return true, nil
	}
	//log.Printf("Non-HTTP2 preface: %v", c.preface)
	return false, nil
}

// Implements net.Listener. Accept() returns a connection queued by add().
type connectionQueue struct {
	addr   net.Addr
	conns  chan net.Conn
	closed bool
	mu     sync.Mutex
}

func newConnectionQueue(addr net.Addr) *connectionQueue {
	return &connectionQueue{addr: addr, conns: make(chan net.Conn, 1), closed: false}
}

// Accept waits for and returns the next connection to the listener.
func (q *connectionQueue) Accept() (net.Conn, error) {
	conn, more := <-q.conns
	if !more {
		return nil, errors.New("Close() called")
	}
	return conn, nil
}

func (q *connectionQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		close(q.conns)
		q.closed = true
	}
	return nil
}

func (q *connectionQueue) Addr() net.Addr {
	return q.addr
}

func (q *connectionQueue) add(conn net.Conn) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.conns <- conn
	}
}
