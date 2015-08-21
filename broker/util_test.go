package broker

import (
	"reflect"
	"testing"

	e "google/emulators"
)

func TestNewPortRangePicker_WhenInvalid(t *testing.T) {
	cases := [][]*e.PortRange{
		[]*e.PortRange{&e.PortRange{Begin: -1, End: 1}},
		[]*e.PortRange{&e.PortRange{Begin: 0, End: 1}},
		[]*e.PortRange{&e.PortRange{Begin: 2, End: 1}},
		[]*e.PortRange{&e.PortRange{Begin: 1, End: 2}, &e.PortRange{Begin: 1, End: 3}},
		[]*e.PortRange{&e.PortRange{Begin: 2, End: 3}, &e.PortRange{Begin: 1, End: 3}},
	}
	for _, c := range cases {
		p, err := NewPortRangePicker(c)
		if err == nil {
			t.Errorf("Expected error for case: %s", c)
		}
		if p != nil {
			t.Errorf("Expected picker to be nil: %s", p)
		}
	}
}

type PickCase struct {
	ranges        []*e.PortRange
	expectedPorts []int
}

func TestPortRangePicker(t *testing.T) {
	cases := []PickCase{
		PickCase{ranges: []*e.PortRange{&e.PortRange{1, 2}}, expectedPorts: []int{1}},
		PickCase{ranges: []*e.PortRange{&e.PortRange{1, 3}}, expectedPorts: []int{1, 2}},
		PickCase{ranges: []*e.PortRange{&e.PortRange{1, 3}, &e.PortRange{3, 5}}, expectedPorts: []int{1, 2, 3, 4}},
		PickCase{ranges: []*e.PortRange{&e.PortRange{1, 3}, &e.PortRange{7, 9}}, expectedPorts: []int{1, 2, 7, 8}},
	}
	for _, c := range cases {
		p, err := NewPortRangePicker(c.ranges)
		if err != nil {
			t.Errorf("Unexpected failure creating picker for %v: %v", c, err)
			continue
		}
		var ports []int
		for true {
			port, err := p.Next()
			if err != nil {
				break
			}
			ports = append(ports, port)
		}
		if !reflect.DeepEqual(ports, c.expectedPorts) {
			t.Errorf("Expected %v: %v", c.expectedPorts, ports)
		}
	}
}
