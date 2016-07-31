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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	glog "github.com/golang/glog"
	emulators "google/emulators"
	http2 "golang.org/x/net/http2"
)

const (
	// The name of the environment variable with the broker's address.
	BrokerAddressEnv = "TESTENV_BROKER_ADDRESS"
)

var (
	// The HTTP/2 client preface
	http2ClientPreface = []byte(http2.ClientPreface)
)

// Returns the broker port from BrokerAddressEnv, or 0.
func BrokerPortFromEnv() int {
	addr := os.Getenv(BrokerAddressEnv)
	if addr == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		glog.Warningf("Invalid format for %s: %s", BrokerAddressEnv, addr)
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		glog.Warningf("Invalid format for %s: %s", BrokerAddressEnv, addr)
		return 0
	}
	return port
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

// An http.ResponseWriter that formats Json content for human-readability.
// Writes are buffered, then flushed to the underlying writer with Flush().
type prettyJsonWriter struct {
	delegate http.ResponseWriter
	indent   string
	buf      bytes.Buffer
}

func (w *prettyJsonWriter) Header() http.Header {
	return w.delegate.Header()
}

func (w *prettyJsonWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *prettyJsonWriter) WriteHeader(code int) {
	w.delegate.WriteHeader(code)
}

func (w *prettyJsonWriter) Flush() {
	var indented bytes.Buffer
	src, _ := ioutil.ReadAll(&w.buf)
	err := json.Indent(&indented, src, "", w.indent)
	if err != nil {
		// Content might not be Json. Just write it as-is.
		w.delegate.Write(src)
		return
	}
	indented.WriteString("\n")
	w.delegate.Write(indented.Bytes())
}

// An http.Handler that formats Json content for human-readability.
type prettyJsonHandler struct {
	delegate http.Handler
	indent   string
}

func (h *prettyJsonHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prettyWriter := &prettyJsonWriter{delegate: w, indent: h.indent}
	h.delegate.ServeHTTP(prettyWriter, r)
	prettyWriter.Flush()
}

// listenerMux implements net.Listener, and multiplexes between HTTP/1.x and
// HTTP/2 connections. HTTPListener will offer HTTP/1.x connections, and
// HTTP2Listener will offer HTTP/2 connections.
type listenerMux struct {
	// Receives only HTTP/1.x connections.
	HTTPListener net.Listener

	// Receives only HTTP/2 connections.
	HTTP2Listener net.Listener

	// The underlying listener.
	delegate net.Listener
}

// newListenerMux creates a listenerMux with the given listener as the
// connection factory.
func newListenerMux(delegate net.Listener) *listenerMux {
	mux := listenerMux{
		HTTPListener:  newConnQueue(delegate.Addr()),
		HTTP2Listener: newConnQueue(delegate.Addr()),
		delegate:      delegate}
	go mux.run()
	return &mux
}

func incrementDelay(delay time.Duration) time.Duration {
	if delay == 0 {
		delay = 5 * time.Millisecond
	} else {
		delay *= 2
	}
	if max := 1 * time.Second; delay > max {
		delay = max
	}
	return delay
}

// Accepts connections from the delegate listener, determines whether they are
// HTTP/1 or HTTP/2, and then attaches them to the appropriate user-facing
// listener.
func (mux *listenerMux) run() error {
	// Code lifted from http module's Serve().
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		conn, e := mux.delegate.Accept()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				tempDelay = incrementDelay(tempDelay)
				glog.V(2).Infof("Accept error: %v; retrying in %v", e, tempDelay)
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
			go mux.HTTP2Listener.(*connQueue).add(connWrapper)
		} else {
			go mux.HTTPListener.(*connQueue).add(connWrapper)
		}
	}
}

// Close the delegate listener and both user-facing listeners.
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
	glog.V(3).Infof("Read(): %d, %v, %q", n, err, b[:n])
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
				glog.V(2).Infof("Error reading preface: %v", err)
				return false, err
			}
			c.readPreface = true
			break
		}
	}
	if bytes.Equal(c.preface, http2ClientPreface) {
		return true, nil
	}
	return false, nil
}

// connQueue represents a queue of connections that are waiting to be
// accepted. Implements net.Listener.
type connQueue struct {
	addr   net.Addr
	conns  chan net.Conn
	closed bool
	mu     sync.Mutex
}

func newConnQueue(addr net.Addr) *connQueue {
	return &connQueue{addr: addr, conns: make(chan net.Conn, 1), closed: false}
}

// Accept returns the next connection in the queue, waiting for one to be added
// if the queue is empty.
func (q *connQueue) Accept() (net.Conn, error) {
	conn, more := <-q.conns
	if !more {
		return nil, errors.New("Close() called")
	}
	return conn, nil
}

func (q *connQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		close(q.conns)
		q.closed = true
	}
	return nil
}

func (q *connQueue) Addr() net.Addr {
	return q.addr
}

// add queues a connection which can subsequently be returned by Accept().
func (q *connQueue) add(conn net.Conn) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.conns <- conn
	}
}
