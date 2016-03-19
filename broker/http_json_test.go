package broker

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	glog "github.com/golang/glog"
	jsonpb "github.com/golang/protobuf/jsonpb"
	proto "github.com/golang/protobuf/proto"
	emulators "google/emulators"
)

// Test-only client.
type httpJsonClient struct {
	port   int
	client http.Client
}

func (c *httpJsonClient) get(url string, output proto.Message) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("Request failed: " + resp.Status)
	}
	if output != nil {
		err = jsonpb.Unmarshal(resp.Body, output)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *httpJsonClient) post(url string, req proto.Message, output proto.Message) error {
	return c.do("POST", url, req, output)
}

// req, err := http.NewRequest("PATCH", url, strings.NewReader(body))
// if err != nil {
//   return nil, err
// }
// resp, err := c.client.Do(req)

func (c *httpJsonClient) patch(url string, req proto.Message, output proto.Message) error {
	return c.do("PATCH", url, req, output)
}

func (c *httpJsonClient) do(method string, url string, req proto.Message, output proto.Message) error {
	var reqBody io.Reader = nil
	if req != nil {
		m := jsonpb.Marshaler{}
		body, err := m.MarshalToString(req)
		if err != nil {
			return err
		}
		glog.Infof("Request body: %v", body)
		reqBody = bytes.NewBufferString(body)
	}
	httpReq, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Request failed [%v]: %v", req, resp.Status)
		}
		return fmt.Errorf("Request failed [%v]: %v: %s", req, resp.Status, b)
	}
	if output != nil {
		err = jsonpb.Unmarshal(resp.Body, output)
		if err != nil {
			return err
		}
	}
	return nil
}

// Waits until the HTTP server is ready. Requests sent before the server is
// ready may result in a variety of error responses (4xx and 5xx).
func (c *httpJsonClient) awaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := c.listEmulators()
		if err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Timed-out waiting for client to become ready!")
}

func (c *httpJsonClient) createEmulator(emu *emulators.Emulator) error {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators", c.port)
	return c.post(url, emu, EmptyPb)
}

func (c *httpJsonClient) getEmulator(id string) (*emulators.Emulator, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators/%s", c.port, id)
	emu := &emulators.Emulator{}
	err := c.get(url, emu)
	return emu, err
}

func (c *httpJsonClient) listEmulators() (*emulators.ListEmulatorsResponse, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators", c.port)
	listResp := &emulators.ListEmulatorsResponse{}
	err := c.get(url, listResp)
	return listResp, err
}

func (c *httpJsonClient) startEmulator(id string) error {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators/%s:start", c.port, id)
	return c.post(url, nil, nil)
}

func (c *httpJsonClient) reportEmulatorOnline(report *emulators.ReportEmulatorOnlineRequest) error {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators/%s:report_online", c.port, report.EmulatorId)
	return c.post(url, report, nil)
}

func (c *httpJsonClient) stopEmulator(id string) error {
	url := fmt.Sprintf("http://localhost:%d/v1/emulators/%s:stop", c.port, id)
	return c.post(url, nil, nil)
}

func (c *httpJsonClient) createResolveRule(rule *emulators.ResolveRule) error {
	url := fmt.Sprintf("http://localhost:%d/v1/resolve_rules", c.port)
	return c.post(url, rule, nil)
}

func (c *httpJsonClient) getResolveRule(id string) (*emulators.ResolveRule, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/resolve_rules/%s", c.port, id)
	rule := &emulators.ResolveRule{}
	err := c.get(url, rule)
	return rule, err
}

func (c *httpJsonClient) listResolveRules() (*emulators.ListResolveRulesResponse, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/resolve_rules", c.port)
	listResp := &emulators.ListResolveRulesResponse{}
	err := c.get(url, listResp)
	return listResp, err
}

func (c *httpJsonClient) updateResolveRule(rule *emulators.ResolveRule) (*emulators.ResolveRule, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/resolve_rules/%s", c.port, rule.RuleId)
	updatedRule := &emulators.ResolveRule{}
	err := c.patch(url, rule, updatedRule)
	return updatedRule, err
}

func (c *httpJsonClient) resolve(req *emulators.ResolveRequest) (*emulators.ResolveResponse, error) {
	url := fmt.Sprintf("http://localhost:%d/v1/resolve_rules:resolve", c.port)
	resp := &emulators.ResolveResponse{}
	err := c.post(url, req, resp)
	return resp, err
}

// Sanity check HTTP/Json access to the emulators resource.
// Tests create, get, and list.
func TestHttpJson_EmulatorsResource(t *testing.T) {
	b, err := startNewBroker(brokerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	c := httpJsonClient{port: b.Port()}
	err = c.awaitReady(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	listResp, err := c.listEmulators()
	if err != nil {
		t.Fatal(err)
	}
	if len(listResp.Emulators) != 0 {
		t.Fatalf("Expected zero emulators: %v", listResp)
	}
	err = c.createEmulator(dummyEmulator)
	if err != nil {
		t.Fatal(err)
	}
	emu, err := c.getEmulator(dummyEmulator.EmulatorId)
	if !proto.Equal(emu, dummyEmulator) {
		t.Fatalf("Expected %v: %v", dummyEmulator, emu)
	}
	listResp, err = c.listEmulators()
	if err != nil {
		t.Fatal(err)
	}
	if len(listResp.Emulators) != 1 {
		t.Fatalf("Expected 1 emulator: %v", listResp)
	}
}

// Sanity check HTTP/Json access to the resolve_rules resource.
// Tests create, get, list, and update.
func TestHttpJson_ResolveRulesResource(t *testing.T) {
	b, err := startNewBroker(brokerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	c := httpJsonClient{port: b.Port()}
	err = c.awaitReady(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	listResp, err := c.listResolveRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listResp.Rules) != 0 {
		t.Fatalf("Expected zero rule: %v", listResp)
	}
	err = c.createResolveRule(dummyEmulator.Rule)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := c.getResolveRule(dummyEmulator.Rule.RuleId)
	if err != nil {
		t.Fatal(err)
	}
	listResp, err = c.listResolveRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(listResp.Rules) != 1 {
		t.Fatalf("Expected 1 rule: %v", listResp)
	}
	rule.TargetPatterns = append(rule.TargetPatterns, "new")
	updatedRule, err := c.updateResolveRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	if !unorderedEqual(updatedRule.TargetPatterns, rule.TargetPatterns) {
		t.Fatalf("Expected %v: %v", rule.TargetPatterns, updatedRule.TargetPatterns)
	}
}

// Tests emulators:start, :report online, and :stop.
func TestHttpJson_EmulatorsCustomMethods(t *testing.T) {
	b, err := startNewBroker(brokerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	c := httpJsonClient{port: b.Port()}
	err = c.awaitReady(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	realNoReg := proto.Clone(realEmulator).(*emulators.Emulator)
	realNoReg.StartCommand.Args = []string{"--port={port:real}"}
	err = c.createEmulator(realNoReg)
	if err != nil {
		t.Fatal(err)
	}

	// Start the emulator, and wait until it is ready to be reported online.
	done := make(chan bool)
	go func() {
		err = c.startEmulator(realNoReg.EmulatorId)
		if err != nil {
			t.Fatal(err)
		}
		done <- true
	}()
	err = b.s.waitForStarting(realNoReg.EmulatorId, time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// Report the emulator online.
	report := &emulators.ReportEmulatorOnlineRequest{
		EmulatorId:   realNoReg.EmulatorId,
		ResolvedHost: "foo"}
	err = c.reportEmulatorOnline(report)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed-out starting emulator!")
	case <-done:
		break
	}
	emu, err := c.getEmulator(realNoReg.EmulatorId)
	if err != nil {
		t.Fatal(err)
	}
	if emu.State != emulators.Emulator_ONLINE {
		t.Fatalf("Expected %v: %v", emulators.Emulator_ONLINE, emu.State)
	}

	// Stop the emulator.
	err = c.stopEmulator(realNoReg.EmulatorId)
	if err != nil {
		t.Fatal(err)
	}
	emu, err = c.getEmulator(realNoReg.EmulatorId)
	if err != nil {
		t.Fatal(err)
	}
	if emu.State != emulators.Emulator_OFFLINE {
		t.Fatalf("Expected %v: %v", emulators.Emulator_OFFLINE, emu.State)
	}
}

// Tests resolve_rules:resolve.
func TestHttpJson_Resolve(t *testing.T) {
	b, err := startNewBroker(brokerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()

	c := httpJsonClient{port: b.Port()}
	err = c.awaitReady(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rule := &emulators.ResolveRule{RuleId: "r0", TargetPatterns: []string{"foo"}, ResolvedHost: "bar"}
	err = c.createResolveRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.getResolveRule("r0")
	if err != nil {
		t.Fatal(err)
	}
	resolveResp, err := c.resolve(&emulators.ResolveRequest{Target: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if resolveResp.Target != "bar" {
		t.Fatalf("Expected bar: %v", resolveResp.Target)
	}
}
